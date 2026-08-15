// Bundles the relay Worker source (relay/src/server.ts) plus its partyserver
// dependency into a single ESM module Worker script the Go binary can embed
// via go:embed (see ../embed.go). Also writes a small worker-metadata.json
// carrying the Cloudflare Script Upload API's module-worker metadata (main
// module name, typed Durable Object binding, single-step migration,
// compatibility date), translated from wrangler.jsonc's config-file shapes
// so `moltnet relay deploy` and `wrangler deploy` describe the same Worker.
//
// Run with `npm run bundle`. Output is committed to relay/dist/ so `go
// build` never needs Node; internal/transport's freshness-test precedent
// (web/) is mirrored by internal/relaydeploy/relay_bundle_freshness_test.go.
import { build } from "esbuild";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const relayRoot = fileURLToPath(new URL(".", import.meta.url));

// wrangler.jsonc is JSONC (allows // line comments); strip them before
// parsing since none of our string values contain "//".
const wranglerRaw = await readFile(new URL("./wrangler.jsonc", import.meta.url), "utf8");
const wranglerConfig = JSON.parse(wranglerRaw.replace(/^\s*\/\/.*$/gm, ""));

await mkdir(new URL("./dist", import.meta.url), { recursive: true });

await build({
  entryPoints: [`${relayRoot}src/server.ts`],
  bundle: true,
  format: "esm",
  // "browser" platform (not "neutral"/"node") makes esbuild honor the
  // "browser" package.json field/export condition, which is how nanoid
  // (a partyserver dependency) picks Web Crypto over node:crypto. The
  // Workers runtime implements the same browser-like globals (fetch,
  // crypto, WebSocket) that this condition targets.
  platform: "browser",
  target: "es2022",
  minify: false,
  sourcemap: false,
  outfile: `${relayRoot}dist/worker.js`,
  // Provided by the Workers runtime itself, not bundled dependencies.
  external: ["cloudflare:workers", "cloudflare:*"],
  logLevel: "info"
});

// The Cloudflare Script Upload API (PUT /accounts/:id/workers/scripts/:name)
// wants a flat, typed `bindings` array — not wrangler.jsonc's nested
// `durable_objects: { bindings: [...] }` config-file shape.
const durableObjectBindings = (wranglerConfig.durable_objects?.bindings ?? []).map((binding) => ({
  type: "durable_object_namespace",
  name: binding.name,
  class_name: binding.class_name
}));

// The Script Upload API also wants a single-step migration object keyed by
// `new_tag`, not wrangler.jsonc's `migrations` array of `{ tag, ... }`
// entries. Take the last configured migration (the one that applies to the
// current class set) and rename its `tag` to `new_tag`, carrying its class
// lists (new_sqlite_classes, etc.) through unchanged.
const wranglerMigrations = wranglerConfig.migrations ?? [];
const lastMigration = wranglerMigrations[wranglerMigrations.length - 1];
const migrations = lastMigration
  ? (({ tag, ...classLists }) => ({ new_tag: tag, ...classLists }))(lastMigration)
  : undefined;

const metadata = {
  main_module: "worker.js",
  compatibility_date: wranglerConfig.compatibility_date,
  bindings: durableObjectBindings,
  ...(migrations ? { migrations } : {})
};

await writeFile(
  new URL("./dist/worker-metadata.json", import.meta.url),
  JSON.stringify(metadata, null, 2) + "\n"
);

console.log("wrote relay/dist/worker.js and relay/dist/worker-metadata.json");
