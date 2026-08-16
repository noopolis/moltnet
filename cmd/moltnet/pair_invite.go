package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/relaydeploy"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// runPairInvite implements `moltnet pair invite`: it generates one
// shareable invite for one friend network against an already-deployed
// relay. The relay's URL and token come from --relay-url/--relay-token-env
// when given, and otherwise from the credentials `moltnet relay deploy`
// last saved to .moltnet/relay.json (see resolvePairInviteRelayCredentials).
func runPairInvite(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("moltnet pair invite", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		relayURLFlag  = flags.String("relay-url", "", "relay Worker URL, e.g. wss://moltnet-relay.acme.workers.dev (default: the relay from the last `moltnet relay deploy`)")
		relayTokenEnv = flags.String("relay-token-env", "", "environment variable containing the relay's RELAY_TOKEN (default: the token from the last `moltnet relay deploy`)")
		pairingID     = flags.String("id", "", "pairing id to use locally and embed in the invite (default: generated)")
		configPath    = flags.String("config", "", "Moltnet config path")
		networkID     = flags.String("network-id", "", "network id to select under ~/.moltnet when several exist")
		printOnly     = flags.Bool("print-only", false, "print the invite code without writing local config")
		force         = flags.Bool("force", false, "overwrite an existing pairing with the same id, generating a fresh invite")
		restart       = flags.Bool("restart", false, "restart the managed service for the resolved network after writing the pairing")
	)
	var sharedRooms repeatedStringFlag
	flags.Var(&sharedRooms, "room", "shared room id to declare in the invite (repeatable or comma-separated)")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("pair invite does not accept positional arguments")
	}

	path, err := resolvePairConfigPath(*configPath, *networkID)
	if err != nil {
		return err
	}

	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return err
	}
	if config.NetworkID == app.DefaultNetworkID {
		return fmt.Errorf(
			"network.id in %s is still the default %q; set a distinct network id before generating an invite (two default installs would collide)",
			path, app.DefaultNetworkID,
		)
	}

	relayURL, relayToken, err := resolvePairInviteRelayCredentials(path, config.NetworkID, *relayURLFlag, *relayTokenEnv)
	if err != nil {
		return err
	}

	id := strings.TrimSpace(*pairingID)
	if id == "" {
		id, err = defaultPairingID()
		if err != nil {
			return err
		}
	}

	room, err := app.GenerateRandomToken(128)
	if err != nil {
		return err
	}
	pairingToken, err := app.GenerateRandomToken(256)
	if err != nil {
		return err
	}

	invite := protocol.Invite{
		V:            protocol.InviteVersion,
		RelayURL:     relayURL,
		Room:         room,
		RelayToken:   relayToken,
		PairingToken: pairingToken,
		PairingID:    id,
		NetworkID:    config.NetworkID,
		NetworkName:  config.NetworkName,
		SharedRooms:  []string(sharedRooms),
		Exp:          time.Now().UTC().Add(protocol.DefaultInviteTTL),
	}
	if err := invite.Validate(); err != nil {
		return fmt.Errorf("generated invite failed validation: %w", err)
	}
	code, err := protocol.EncodeInvite(invite)
	if err != nil {
		return err
	}

	if *printOnly {
		fmt.Fprintln(stdout, code)
		return nil
	}

	pairing := app.PairingWriteback{
		ID:    id,
		Token: pairingToken,
		Relay: &app.PairingRelayWriteback{
			URL:   invite.RelayURL,
			Room:  room,
			Token: relayToken,
		},
	}
	authToken := app.AuthTokenWriteback{
		ID:     id,
		Value:  pairingToken,
		Scopes: []string{"pair"},
	}

	if err := writePairingWithRollback(path, pairing, authToken, invite.SharedRooms, *force); err != nil {
		if errors.Is(err, app.ErrPairingExists) {
			return fmt.Errorf("%w, or choose a different --id", err)
		}
		return err
	}

	return printPairInviteAftercare(ctx, config, path, code, id, pairAftercareOptions{
		restart: *restart,
		roomIDs: invite.SharedRooms,
		// remoteNetworkID/remoteNetworkName are unknown here: pair invite
		// never learns the friend's identity (no round trip); `pair <code>`
		// on their side prints the real, filled-in command instead.
	})
}

// defaultPairingID generates a short, readable pairing id when --id is not
// supplied.
func defaultPairingID() (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generate pairing id: %w", err)
	}
	return "friend-" + hex.EncodeToString(suffix), nil
}

// resolvePairInviteRelayCredentials resolves the relay URL and token
// `pair invite` embeds in the generated invite. Each of relayURLFlag and
// relayTokenEnvFlag, if non-empty, wins outright for its half of the pair;
// whichever half is left unset falls back to the credentials stored at
// relaydeploy.CredentialsPath(configPath) by the last successful
// `moltnet relay deploy` (PLAN.md's "zero-flag path"). networkID is the
// already-resolved network id, threaded into the "run `moltnet relay
// deploy`" guidance so it names the exact command for this network rather
// than a bare one that silently targets the wrong network in a multi-network
// home.
func resolvePairInviteRelayCredentials(configPath, networkID, relayURLFlag, relayTokenEnvFlag string) (relayURL string, relayToken string, err error) {
	relayURL = strings.TrimSpace(relayURLFlag)

	if strings.TrimSpace(relayTokenEnvFlag) != "" {
		relayToken = strings.TrimSpace(os.Getenv(relayTokenEnvFlag))
		if relayToken == "" {
			return "", "", fmt.Errorf("environment variable %q named by --relay-token-env is empty or not set", relayTokenEnvFlag)
		}
	}

	if relayURL != "" && relayToken != "" {
		return relayURL, relayToken, nil
	}

	credentialsPath := relaydeploy.CredentialsPath(configPath)
	stored, ok, loadErr := relaydeploy.LoadCredentials(credentialsPath)
	if loadErr != nil {
		return "", "", loadErr
	}

	if relayURL == "" {
		if !ok || strings.TrimSpace(stored.URL) == "" {
			return "", "", fmt.Errorf(
				"pair invite requires --relay-url (or run `moltnet relay deploy --id %s` first to save one to %s)", networkID, credentialsPath,
			)
		}
		relayURL = strings.TrimSpace(stored.URL)
	}
	if relayToken == "" {
		if !ok || strings.TrimSpace(stored.Token) == "" {
			return "", "", fmt.Errorf(
				"pair invite requires --relay-token-env (or run `moltnet relay deploy --id %s` first to save a token to %s)", networkID, credentialsPath,
			)
		}
		relayToken = strings.TrimSpace(stored.Token)
	}

	return relayURL, relayToken, nil
}
