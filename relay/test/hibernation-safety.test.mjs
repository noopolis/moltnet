import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// `wrangler dev --local` never evicts a Durable Object, so no behavioural test
// in this suite can prove that admission state survives hibernation. These
// checks enforce the rule that makes survival true by construction instead:
// admission lives in each socket's serialized attachment, never in an instance
// field. Reintroducing the pre-hibernation `admittedPeers` Set passes every
// other test in this directory and is broken in production after ~10s idle.
const source = readFileSync(new URL("../src/server.ts", import.meta.url), "utf8");
const relayRoom = source.slice(source.indexOf("export class RelayRoom"), source.indexOf("\nexport default"));

test("hibernation stays enabled", () => {
  assert.match(relayRoom, /static options = \{ hibernate: true \}/);
});

test("RelayRoom holds no instance state", () => {
  // A field declaration assigns or type-annotates; a method is followed by `(`.
  const fields = [...relayRoom.matchAll(/^ {2}(?!static )(?:(?:private|public|protected|readonly) )*[A-Za-z_$][\w$]*\s*[:=]/gm)].map(
    (match) => match[0].trim()
  );
  assert.deepEqual(fields, [], `instance fields do not survive eviction: ${fields.join(", ")}`);
});

test("admission is read from connection state, not a collection", () => {
  assert.match(relayRoom, /connection\.setState\(\{ admitted: true \}\)/);
  assert.doesNotMatch(relayRoom, /new (?:Set|Map|WeakSet|WeakMap)\s*[<(]/);
});

test("peers are compared by object identity, never by client-supplied id", () => {
  // `id` comes from the `?_pk=` query parameter, so two admitted peers can
  // collide on one; identity comparison is what keeps peer selection sound.
  assert.doesNotMatch(relayRoom, /\.id\s*[!=]==?\s*\w+\.id/);
  assert.match(relayRoom, /candidate !== (?:self|connection)/);
});
