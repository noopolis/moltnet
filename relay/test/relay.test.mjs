import assert from "node:assert/strict";
import { createHash, randomBytes } from "node:crypto";
import { once } from "node:events";
import net from "node:net";
import { spawn } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const relayDirectory = fileURLToPath(new URL("..", import.meta.url));
const host = "127.0.0.1";
const token = "development-relay-token";
let worker;
let port;

test.before(async () => {
  port = await availablePort();
  worker = spawn(
    "./node_modules/.bin/wrangler",
    ["dev", "--local", "--port", String(port), "--var", `RELAY_TOKEN:${token}`],
    {
      cwd: relayDirectory,
      stdio: ["ignore", "pipe", "pipe"]
    }
  );
  await waitForWorker();
});

test.after(async () => {
  if (!worker || worker.exitCode !== null) return;
  worker.kill("SIGTERM");
  await once(worker, "exit");
});

test("relays opaque req and res frames between exactly two authenticated peers", async (t) => {
  const first = await RawWebSocket.connect({ token });
  const second = await RawWebSocket.connect({ token });
  t.after(() => first.close());
  t.after(() => second.close());

  first.send(JSON.stringify({ t: "hello", network: "network-a" }));
  second.send(JSON.stringify({ t: "hello", network: "network-b" }));
  await new Promise((resolve) => setTimeout(resolve, 20));

  // This is deliberately invalid JSON. It is valid only as an opaque body
  // string, so a relay that attempts to decode the body cannot round-trip it.
  const request = JSON.stringify({
    t: "req",
    id: "opaque-request",
    to: "network-b",
    body: '{"unclosed":'
  });
  first.send(request);
  assert.equal(await second.nextText(), request);

  const response = JSON.stringify({
    t: "res",
    id: "opaque-request",
    to: "network-a",
    body: "not a protocol payload: >>>"
  });
  second.send(response);
  assert.equal(await first.nextText(), response);
});

test("refuses invalid bearer tokens and a third peer", async (t) => {
  await assert.rejects(RawWebSocket.connect({ token: "wrong-token" }), /401 Unauthorized/);

  const first = await RawWebSocket.connect({ token, room: "capacity" });
  const second = await RawWebSocket.connect({ token, room: "capacity" });
  const third = await RawWebSocket.connect({ token, room: "capacity" });
  t.after(() => first.close());
  t.after(() => second.close());
  t.after(() => third.close());

  const closed = await third.nextClose();
  assert.equal(closed.code, 1013);
});

async function waitForWorker() {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (worker.exitCode !== null) {
      throw new Error("wrangler dev exited before becoming ready");
    }
    try {
      const response = await fetch(`http://${host}:${port}/`);
      if (response.status === 404) return;
    } catch {
      // The local runtime has not bound its port yet.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("wrangler dev did not become ready");
}

function availablePort() {
  return new Promise((resolve, reject) => {
    const listener = net.createServer();
    listener.once("error", reject);
    listener.listen(0, host, () => {
      const address = listener.address();
      if (typeof address !== "object" || address === null) {
        listener.close();
        reject(new Error("could not select a local port"));
        return;
      }
      listener.close((error) => (error ? reject(error) : resolve(address.port)));
    });
  });
}

class RawWebSocket {
  #socket;
  #buffer = Buffer.alloc(0);
  #events = [];
  #waiters = [];

  static async connect({ token: bearerToken, room = "opaque" }) {
    const socket = net.connect(port, host);
    await once(socket, "connect");

    const key = randomBytes(16).toString("base64");
    const request = [
      `GET /parties/relay-room/${room} HTTP/1.1`,
      `Host: ${host}:${port}`,
      "Upgrade: websocket",
      "Connection: Upgrade",
      `Sec-WebSocket-Key: ${key}`,
      "Sec-WebSocket-Version: 13",
      `Authorization: Bearer ${bearerToken}`,
      "",
      ""
    ].join("\r\n");
    socket.write(request);

    const response = await readUpgradeResponse(socket);
    if (!response.startsWith("HTTP/1.1 101")) {
      socket.destroy();
      throw new Error(response.split("\r\n", 1)[0]);
    }
    const accept = response.match(/sec-websocket-accept:\s*(.+)\r?$/im)?.[1]?.trim();
    const expected = createHash("sha1")
      .update(`${key}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
      .digest("base64");
    assert.equal(accept, expected, "server accepted the WebSocket handshake");
    return new RawWebSocket(socket);
  }

  constructor(socket) {
    this.#socket = socket;
    socket.on("data", (chunk) => this.#receive(chunk));
    socket.on("close", () => this.#emit({ type: "close", code: 1006 }));
    socket.on("error", (error) => this.#emit({ type: "error", error }));
  }

  send(text) {
    const body = Buffer.from(text);
    const mask = randomBytes(4);
    const header = frameHeader(0x81, body.length, true);
    for (let index = 0; index < body.length; index += 1) body[index] ^= mask[index % 4];
    this.#socket.write(Buffer.concat([header, mask, body]));
  }

  async nextText() {
    const event = await this.#next("text");
    return event.text;
  }

  async nextClose() {
    return this.#next("close");
  }

  close() {
    if (!this.#socket.destroyed) this.#socket.end(Buffer.from([0x88, 0x80, 0, 0, 0, 0]));
  }

  #next(type) {
    const existing = this.#events.findIndex((event) => event.type === type);
    if (existing >= 0) return Promise.resolve(this.#events.splice(existing, 1)[0]);
    return new Promise((resolve, reject) => this.#waiters.push({ type, resolve, reject }));
  }

  #emit(event) {
    const waiter = this.#waiters.findIndex((candidate) => candidate.type === event.type);
    if (waiter >= 0) this.#waiters.splice(waiter, 1)[0].resolve(event);
    else this.#events.push(event);
  }

  #receive(chunk) {
    this.#buffer = Buffer.concat([this.#buffer, chunk]);
    while (this.#buffer.length >= 2) {
      const first = this.#buffer[0];
      let length = this.#buffer[1] & 0x7f;
      let offset = 2;
      if (length === 126) {
        if (this.#buffer.length < 4) return;
        length = this.#buffer.readUInt16BE(2);
        offset = 4;
      }
      if (this.#buffer.length < offset + length) return;
      const payload = this.#buffer.subarray(offset, offset + length);
      this.#buffer = this.#buffer.subarray(offset + length);
      if ((first & 0x0f) === 0x1) this.#emit({ type: "text", text: payload.toString() });
      if ((first & 0x0f) === 0x8) this.#emit({ type: "close", code: payload.readUInt16BE() });
    }
  }
}

function frameHeader(first, length, masked) {
  if (length < 126) return Buffer.from([first, (masked ? 0x80 : 0) | length]);
  return Buffer.from([first, (masked ? 0x80 : 0) | 126, length >> 8, length & 0xff]);
}

function readUpgradeResponse(socket) {
  return new Promise((resolve, reject) => {
    let response = Buffer.alloc(0);
    const onData = (chunk) => {
      response = Buffer.concat([response, chunk]);
      const end = response.indexOf("\r\n\r\n");
      if (end < 0) return;
      socket.off("data", onData);
      socket.off("error", onError);
      resolve(response.subarray(0, end + 4).toString());
      const trailing = response.subarray(end + 4);
      if (trailing.length > 0) socket.unshift(trailing);
    };
    const onError = (error) => {
      socket.off("data", onData);
      reject(error);
    };
    socket.on("data", onData);
    socket.once("error", onError);
  });
}
