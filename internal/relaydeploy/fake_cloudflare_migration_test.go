package relaydeploy

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// This file holds fakeCloudflareServer's per-script Durable Object
// migration-tag modeling: the state and handlers backing
// Client.ScriptMigrationTag and the upload precondition check that
// reproduces Cloudflare's real 412/10079 rejection. Split out of
// fake_cloudflare_test.go (handleUploadWorker itself, which calls into
// this) to keep both files under the repo's 400-line split threshold.

// uploadMigrations mirrors the shape of the upload metadata's "migrations"
// key: {"old_tag":"...","new_tag":"..."} with old_tag omitted (so decoding
// to "") on a from-scratch migration, exactly how relay.WorkerMetadataJSON
// is authored and how prepareUploadMetadata's stripped/unchanged output
// looks on the wire.
type uploadMigrations struct {
	OldTag string `json:"old_tag"`
	NewTag string `json:"new_tag"`
}

// parseUploadMigrations reports whether metadata carries a top-level
// "migrations" key and, if so, decodes it. Absent entirely (present=false)
// is distinct from an empty object: prepareUploadMetadata's redeploy-over-
// an-already-migrated-script path omits the key altogether, and that
// omission is exactly what must skip the precondition check below.
func parseUploadMigrations(metadata []byte) (migrations uploadMigrations, present bool, err error) {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &parsed); err != nil {
		return uploadMigrations{}, false, err
	}
	raw, present := parsed["migrations"]
	if !present {
		return uploadMigrations{}, false, nil
	}
	if err := json.Unmarshal(raw, &migrations); err != nil {
		return uploadMigrations{}, false, err
	}
	return migrations, true, nil
}

// checkMigrationPrecondition mirrors Cloudflare's real Script Upload API
// precondition check on an embedded Durable Object migration: when the
// upload metadata carries a "migrations" key, its old_tag (defaulting to
// "" when omitted) must match scriptName's actual current migration tag —
// otherwise the upload is rejected, exactly the field failure this whole
// fix exists for (`moltnet relay deploy` used to send the embedded
// migration unconditionally, so a redeploy over an already-migrated script
// always sent old_tag "" against a script actually at tag "v1"). No
// "migrations" key at all skips the check entirely — the fix's
// redeploy-over-an-already-migrated-script path.
func (f *fakeCloudflareServer) checkMigrationPrecondition(scriptName string, metadata []byte) error {
	migrations, present, err := parseUploadMigrations(metadata)
	if err != nil || !present {
		// A malformed migrations value is caught separately by
		// validateUploadWorkerMetadataShape before this is ever reached.
		return nil
	}
	f.mu.Lock()
	actualTag := f.scriptMigrationTags[scriptName]
	f.mu.Unlock()
	if migrations.OldTag != actualTag {
		return fmt.Errorf("Actor migration tag precondition failed, got tag '%s' when expected tag is '%s'", migrations.OldTag, actualTag)
	}
	return nil
}

// applyMigrationLocked records scriptName's post-upload migration state.
// Callers must hold f.mu. A script that didn't exist before this upload now
// does (present in the map, even with tag "" if this upload carried no
// migrations key); a migrations key present in metadata additionally
// advances the tag to its new_tag, mirroring Cloudflare applying the
// migration on a successful upload.
func (f *fakeCloudflareServer) applyMigrationLocked(scriptName string, metadata []byte) {
	if _, exists := f.scriptMigrationTags[scriptName]; !exists {
		f.scriptMigrationTags[scriptName] = ""
	}
	if migrations, present, err := parseUploadMigrations(metadata); err == nil && present {
		f.scriptMigrationTags[scriptName] = migrations.NewTag
	}
}

// handleGetService backs Client.ScriptMigrationTag (GET
// /accounts/{account_id}/workers/services/{script_name}, Cloudflare's
// per-script "Get Service Metadata" operation): scriptName's current
// migration_tag nested under default_environment.script, matching the real
// API's response shape exactly. A script this fake has never seen an
// upload for (not a key in scriptMigrationTags) is reported as not-found
// via Cloudflare's documented error code 10092
// ("workers.api.error.environment_not_found") — see
// isServiceNotFoundEnvelope's doc comment in client.go for why 10092
// specifically, among the several codes that operation can return.
func (f *fakeCloudflareServer) handleGetService(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	scriptName := r.PathValue("script")

	f.mu.Lock()
	tag, exists := f.scriptMigrationTags[scriptName]
	f.mu.Unlock()
	if !exists {
		writeCloudflareEnvelope(w, http.StatusNotFound, false, nil, []CloudflareMessage{{Code: 10092, Message: "workers.api.error.environment_not_found"}})
		return
	}

	result, err := json.Marshal(fakeServiceMetadata{
		DefaultEnvironment: fakeServiceMetadataEnvironment{
			Script: fakeServiceMetadataScript{MigrationTag: tag},
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeCloudflareEnvelope(w, http.StatusOK, true, result, nil)
}

// fakeServiceMetadata mirrors the real Cloudflare "Get Service Metadata"
// response shape (ServiceMetadataRes in Cloudflare's own workers-sdk),
// narrowed to the one field ScriptMigrationTag actually reads.
type fakeServiceMetadata struct {
	DefaultEnvironment fakeServiceMetadataEnvironment `json:"default_environment"`
}

type fakeServiceMetadataEnvironment struct {
	Script fakeServiceMetadataScript `json:"script"`
}

type fakeServiceMetadataScript struct {
	MigrationTag string `json:"migration_tag"`
}

// seedScriptMigrationTag pre-registers scriptName as already existing with
// the given migration tag, without driving an actual upload — used to test
// prepareUploadMetadata's unrecognized-tag error branch, which (being a
// single migration step) has no legitimate real-upload path to reach it
// from.
func (f *fakeCloudflareServer) seedScriptMigrationTag(scriptName, tag string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scriptMigrationTags[scriptName] = tag
}
