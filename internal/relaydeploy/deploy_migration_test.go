package relaydeploy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/relay"
)

// This file covers the P1 field fix: `moltnet relay deploy` always sent the
// embedded relay Worker's single-step Durable Object migration
// ({"new_tag":"v1",...}) unconditionally, so redeploying over a script
// already at migration tag "v1" hit Cloudflare's real rejection verbatim:
//
//	upload relay worker: cloudflare api error (http 412): 10079: Actor
//	migration tag precondition failed, got tag '' when expected tag is 'v1'
//
// See upload_metadata.go (prepareUploadMetadata) for the fix and
// fake_cloudflare_migration_test.go for the fake's modeling of this.

// TestDeployFreshCreateSendsMigrationUnchanged is the explicit fresh-create
// counterpart to TestDeployHappyPath's byte-exact metadata assertion: no
// script named scriptName exists yet, so prepareUploadMetadata must return
// relay.WorkerMetadataJSON completely unchanged, migrations key included.
func TestDeployFreshCreateSendsMigrationUnchanged(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})
	_, err := Deploy(context.Background(), fake.client(), Options{ScriptName: "moltnet-relay", ResolveHostname: stubResolveHostname(true)})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if got := string(fake.uploadedMetadata); got != string(relay.WorkerMetadataJSON) {
		t.Fatalf("fresh-create uploaded metadata = %q, want the embedded metadata unchanged", got)
	}
	if !strings.Contains(string(fake.uploadedMetadata), `"migrations"`) {
		t.Fatal(`expected a fresh create to carry the "migrations" key`)
	}

	tag, found, err := fake.client().ScriptMigrationTag(context.Background(), fake.cfg.accountID, "moltnet-relay")
	if err != nil {
		t.Fatalf("ScriptMigrationTag() error = %v", err)
	}
	if !found || tag != "v1" {
		t.Fatalf("ScriptMigrationTag() = (%q, %v), want (v1, true) after the migration applied", tag, found)
	}
}

// TestDeployRedeployOverExistingMigrationOmitsMigrationsKey is the direct
// regression test for the field failure: a second Deploy() against a script
// already migrated to "v1" by the first must send upload metadata with NO
// "migrations" key at all (not an empty one), and must still succeed —
// where the pre-fix code sent the embedded migration unconditionally and
// got the verbatim 412/10079 rejection reproduced by
// TestFakeUploadRejectsUnconditionalMigrationReplayWithFieldObserved412
// below.
func TestDeployRedeployOverExistingMigrationOmitsMigrationsKey(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})
	opts := Options{ScriptName: "moltnet-relay", ResolveHostname: stubResolveHostname(true)}

	first, err := Deploy(context.Background(), fake.client(), opts)
	if err != nil {
		t.Fatalf("first Deploy() error = %v", err)
	}

	opts.PriorToken = first.Token
	second, err := Deploy(context.Background(), fake.client(), opts)
	if err != nil {
		t.Fatalf("redeploy Deploy() error = %v, want the redeploy to succeed (this is the field-observed 412 this fix resolves)", err)
	}
	if second.Token != first.Token {
		t.Fatalf("redeploy token = %q, want unchanged %q", second.Token, first.Token)
	}

	var parsed map[string]any
	if err := json.Unmarshal(fake.uploadedMetadata, &parsed); err != nil {
		t.Fatalf("decode redeploy uploaded metadata: %v", err)
	}
	if _, present := parsed["migrations"]; present {
		t.Fatalf(`redeploy uploaded metadata = %q, want no "migrations" key at all`, fake.uploadedMetadata)
	}
	// Every other key must survive the strip untouched.
	if _, present := parsed["main_module"]; !present {
		t.Fatalf(`redeploy uploaded metadata = %q, want "main_module" preserved`, fake.uploadedMetadata)
	}
	if _, present := parsed["bindings"]; !present {
		t.Fatalf(`redeploy uploaded metadata = %q, want "bindings" preserved`, fake.uploadedMetadata)
	}
}

// TestDeployUnrecognizedMigrationTagFailsClearly covers the third branch:
// a script whose current migration tag matches neither "" (never migrated)
// nor the embedded migration's own new_tag ("v1") has no safe automatic
// path — moltnet relay only ever ships one migration step, so there is no
// chain to replay from. Deploy must fail with an error naming both tags,
// never guess or upload anyway.
func TestDeployUnrecognizedMigrationTagFailsClearly(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})
	fake.seedScriptMigrationTag("moltnet-relay", "some-other-tag")

	_, err := Deploy(context.Background(), fake.client(), Options{ScriptName: "moltnet-relay", ResolveHostname: stubResolveHostname(true)})
	if err == nil {
		t.Fatal("Deploy() error = nil, want a clear error for an unrecognized migration tag")
	}
	if !strings.Contains(err.Error(), "some-other-tag") {
		t.Fatalf("Deploy() error = %v, want it to name the script's actual tag %q", err, "some-other-tag")
	}
	if !strings.Contains(err.Error(), "v1") {
		t.Fatalf("Deploy() error = %v, want it to name the embedded migration tag %q", err, "v1")
	}
	if fake.uploadedScript != nil {
		t.Fatal("expected Deploy to fail before ever uploading the worker for an unrecognized migration tag")
	}
}

// TestDeployClaimThenRerunFreshScriptSendsMigration covers the
// claim-then-rerun flow's "script does not exist yet" branch: an operator
// claims a workers.dev subdomain (e.g. via the interactive prompt) before
// any script named scriptName has ever been uploaded — that first Deploy
// after the claim must still find no script on GET /workers/scripts and
// send the migration exactly as any first-ever deploy would.
func TestDeployClaimThenRerunFreshScriptSendsMigration(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "", claimSubdomain: "apresmoi"})

	if err := fake.client().ClaimWorkersDevSubdomain(context.Background(), fake.cfg.accountID, "apresmoi"); err != nil {
		t.Fatalf("ClaimWorkersDevSubdomain() error = %v", err)
	}

	result, err := Deploy(context.Background(), fake.client(), Options{ScriptName: "moltnet-relay", ResolveHostname: stubResolveHostname(true)})
	if err != nil {
		t.Fatalf("Deploy() after claim error = %v", err)
	}
	if result.ScriptName != "moltnet-relay" {
		t.Fatalf("ScriptName = %q, want moltnet-relay", result.ScriptName)
	}
	if !strings.Contains(string(fake.uploadedMetadata), `"migrations"`) {
		t.Fatalf(`post-claim first deploy uploaded metadata = %q, want the fresh-create "migrations" key present`, fake.uploadedMetadata)
	}
}

// TestDeployClaimThenRerunExistingScriptOmitsMigration covers the
// claim-then-rerun flow's "script already exists" branch. Since the
// claim-state-before-any-mutation fix (deploy.go), Deploy's first attempt on
// an unclaimed account never uploads anything at all — see
// TestDeployWorkersDevSubdomainUnclaimedFailsBeforeAnyUpload — so a script
// that already exists and is migrated to "v1" by the time of the
// claim-then-rerun must have gotten there some other way (e.g. a deploy
// while the account still had a different, since-abandoned claim); this test
// models that by seeding the migration tag directly, then exercises the
// scenario the fix's docstring above cares about: the rerun after claiming
// must find the existing "v1" tag and omit the migrations key, not resend
// it.
func TestDeployClaimThenRerunExistingScriptOmitsMigration(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "", claimSubdomain: "apresmoi"})
	fake.seedScriptMigrationTag("moltnet-relay", "v1")
	opts := Options{ScriptName: "moltnet-relay", ResolveHostname: stubResolveHostname(true)}

	if _, err := Deploy(context.Background(), fake.client(), opts); !errors.Is(err, ErrWorkersDevSubdomainUnclaimed) {
		t.Fatalf("first Deploy() error = %v, want ErrWorkersDevSubdomainUnclaimed", err)
	}
	if fake.uploadedScript != nil {
		t.Fatal("expected no worker upload before the subdomain claim check (the bug this fix closes)")
	}

	if err := fake.client().ClaimWorkersDevSubdomain(context.Background(), fake.cfg.accountID, "apresmoi"); err != nil {
		t.Fatalf("ClaimWorkersDevSubdomain() error = %v", err)
	}

	if _, err := Deploy(context.Background(), fake.client(), opts); err != nil {
		t.Fatalf("rerun Deploy() after claim error = %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(fake.uploadedMetadata, &parsed); err != nil {
		t.Fatalf("decode rerun uploaded metadata: %v", err)
	}
	if _, present := parsed["migrations"]; present {
		t.Fatalf(`rerun-after-claim (existing script) uploaded metadata = %q, want no "migrations" key`, fake.uploadedMetadata)
	}
}

// TestFakeUploadRejectsUnconditionalMigrationReplayWithFieldObserved412
// proves, by construction, that the new fake actually catches the pre-fix
// bug: it drives Client.UploadWorkerModule directly with
// relay.WorkerMetadataJSON completely unmodified — exactly what the old
// deploy() sent on every call, redeploy included, before this fix added
// prepareUploadMetadata — against a script the fake already has at
// migration tag "v1" (seeded the same way a first successful deploy would
// leave it). The fake must reject it with the exact field-reported error:
// HTTP 412, Cloudflare code 10079, message "Actor migration tag
// precondition failed, got tag ” when expected tag is 'v1'".
func TestFakeUploadRejectsUnconditionalMigrationReplayWithFieldObserved412(t *testing.T) {
	t.Parallel()

	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{authOK: true, subdomain: "acme"})
	fake.seedScriptMigrationTag("moltnet-relay", "v1")

	err := fake.client().UploadWorkerModule(context.Background(), fake.cfg.accountID, "moltnet-relay", "worker.js", relay.WorkerScript, relay.WorkerMetadataJSON)
	if err == nil {
		t.Fatal("UploadWorkerModule() error = nil, want the field-observed 412/10079 rejection")
	}

	apiErr, ok := err.(*CloudflareAPIError)
	if !ok {
		t.Fatalf("UploadWorkerModule() error type = %T, want *CloudflareAPIError", err)
	}
	if apiErr.StatusCode != 412 {
		t.Fatalf("UploadWorkerModule() status = %d, want 412", apiErr.StatusCode)
	}
	const wantMessage = "10079: Actor migration tag precondition failed, got tag '' when expected tag is 'v1'"
	if !strings.Contains(apiErr.Error(), wantMessage) {
		t.Fatalf("UploadWorkerModule() error = %q, want it to contain the verbatim field-reported message %q", apiErr.Error(), wantMessage)
	}
	// The full field report, reproduced verbatim end to end (matches the
	// error deploy() itself would have wrapped this in, pre-fix).
	const fieldReportedError = `upload relay worker: cloudflare api error (http 412): 10079: Actor migration tag precondition failed, got tag '' when expected tag is 'v1'`
	if got := "upload relay worker: " + apiErr.Error(); got != fieldReportedError {
		t.Fatalf("reconstructed field error = %q, want %q", got, fieldReportedError)
	}
}
