// Package relay exposes the pre-bundled Cloudflare Worker script built from
// relay/src by `npm run bundle` (see build.mjs). The bundle is committed to
// relay/dist so `go build` never needs Node; rebuild with `npm run bundle`
// in relay/ whenever relay/src, wrangler.jsonc, or build.mjs change. See
// internal/relaydeploy's bundle-freshness test for the CI guard against a
// stale commit.
//
// This file is the only place a Go package reaches into relay/: it embeds
// the built artifact and nothing else, mirroring web/embed.go. relay/ stays
// independently buildable and extractable, and Go code never imports relay
// TypeScript source.
package relay

import _ "embed"

// WorkerScript is the bundled ESM Cloudflare Worker module
// (relay/dist/worker.js): relay/src/server.ts plus its partyserver
// dependency, with no unresolved imports besides the Workers-runtime-provided
// "cloudflare:workers" module.
//
//go:embed dist/worker.js
var WorkerScript []byte

// WorkerMetadataJSON is the Cloudflare module-worker upload metadata
// (relay/dist/worker-metadata.json): main module name, the RelayRoom
// Durable Object binding, its SQLite-backed migration, and the
// compatibility date, mirrored from relay/wrangler.jsonc by build.mjs.
//
//go:embed dist/worker-metadata.json
var WorkerMetadataJSON []byte
