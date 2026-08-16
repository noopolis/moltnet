package relaydeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// prepareUploadMetadata resolves the worker upload metadata Deploy sends
// for scriptName, based on the script's current Durable Object migration
// tag (Client.ScriptMigrationTag). Cloudflare's Script Upload API applies
// the upload metadata's "migrations" key as a one-shot replay: its old_tag
// (defaulting to "" when the key is omitted, exactly how
// embeddedMetadataJSON — relay.WorkerMetadataJSON — is authored) must match
// the script's actual current migration tag, or the upload is rejected
// with error 10079 ("Actor migration tag precondition failed"). That is the
// field-observed 412 this function exists to avoid: every deploy used to
// send embeddedMetadataJSON's single-step {"new_tag":"v1",...} migration
// unconditionally, so any redeploy of a script already at tag "v1" replayed
// a migration Cloudflare had already applied, and the implied old_tag ""
// never matched the script's real tag "v1".
//
//   - No script named scriptName exists yet, or it exists with an
//     empty/unset migration tag: embeddedMetadataJSON is returned
//     unchanged — the fresh-create path, where Cloudflare applies the
//     embedded migration exactly as authored.
//   - The script's current tag already equals the embedded migration's
//     new_tag: that migration was already applied by an earlier deploy, so
//     the "migrations" key is stripped out entirely — Cloudflare's Script
//     Upload API accepts an upload with no migrations key at all, leaving
//     the script's Durable Object bindings untouched.
//   - The script's current tag is anything else non-empty: moltnet relay
//     only ever ships a single migration step, so there is no multi-step
//     chain to replay from an unrecognized tag. This fails loudly, naming
//     both tags, rather than guessing (unlike wrangler's own equivalent
//     check, which sends old_tag as the script's actual unrecognized tag
//     plus every configured migration step as-is, not a from-scratch
//     replay — not a safe default here, since moltnet relay has no
//     multi-step migration history to fall back to).
func prepareUploadMetadata(ctx context.Context, client *Client, accountID, scriptName string, embeddedMetadataJSON []byte) ([]byte, error) {
	embeddedTag, err := embeddedMigrationNewTag(embeddedMetadataJSON)
	if err != nil {
		return nil, err
	}

	currentTag, found, err := client.ScriptMigrationTag(ctx, accountID, scriptName)
	if err != nil {
		return nil, fmt.Errorf("check relay worker migration tag: %w", err)
	}

	if !found || currentTag == "" {
		return embeddedMetadataJSON, nil
	}

	if currentTag == embeddedTag {
		return stripMigrationsKey(embeddedMetadataJSON)
	}

	return nil, fmt.Errorf("relay worker script %q has migration tag %q, which does not match moltnet's embedded migration tag %q; moltnet relay deploy only supports a single migration step and cannot replay from an unrecognized tag", scriptName, currentTag, embeddedTag)
}

// embeddedMigrationNewTag extracts migrations.new_tag from metadataJSON
// (relay.WorkerMetadataJSON), the tag prepareUploadMetadata compares a
// script's current migration tag against.
func embeddedMigrationNewTag(metadataJSON []byte) (string, error) {
	var parsed struct {
		Migrations struct {
			NewTag string `json:"new_tag"`
		} `json:"migrations"`
	}
	if err := json.Unmarshal(metadataJSON, &parsed); err != nil {
		return "", fmt.Errorf("decode embedded worker upload metadata: %w", err)
	}
	if strings.TrimSpace(parsed.Migrations.NewTag) == "" {
		return "", errors.New("embedded worker upload metadata missing migrations.new_tag")
	}
	return parsed.Migrations.NewTag, nil
}

// stripMigrationsKey returns metadataJSON with its top-level "migrations"
// key removed, preserving every other key. Used when the target script's
// migration tag already matches the embedded migration: the redeploy must
// carry no migrations key at all, not merely an empty one — an empty
// {"migrations":{}} object is not a valid Cloudflare migration step.
func stripMigrationsKey(metadataJSON []byte) ([]byte, error) {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(metadataJSON, &parsed); err != nil {
		return nil, fmt.Errorf("decode worker upload metadata: %w", err)
	}
	delete(parsed, "migrations")
	stripped, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("encode worker upload metadata: %w", err)
	}
	return stripped, nil
}
