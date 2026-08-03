import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { once } from "node:events";
import { readFileSync } from "node:fs";
import net from "node:net";
import test from "node:test";
import { fileURLToPath } from "node:url";
// server.ts re-exports these values; import their dependency-free leaf so this
// Node test does not need to load the Cloudflare-only PartyServer runtime.
import { MAX_PAYLOAD_BYTES, MAX_ROUTING_HEADER_BYTES } from "../src/protocol.js";

const relayDirectory = fileURLToPath(new URL("..", import.meta.url));
const fixture = JSON.parse(readFileSync(new URL("../testdata/golden_frames.json", import.meta.url)));
const host = "127.0.0.1";
const token = "golden-frame-relay-token";
let worker;
let port;

test.before(async () => {
  port = await availablePort();
  worker = spawn(
    "./node_modules/.bin/wrangler",
    ["dev", "--local", "--port", String(port), "--var", `RELAY_TOKEN:${token}`],
    { cwd: relayDirectory, stdio: ["ignore", "pipe", "pipe"] }
  );
  await waitForWorker();
});

test.after(async () => {
  if (!worker || worker.exitCode !== null) return;
  worker.kill("SIGTERM");
  await once(worker, "exit");
});

test("golden fixture constants match the Worker exports", () => {
  assert.equal(MAX_ROUTING_HEADER_BYTES, fixture.maxHeaderBytes);
  assert.equal(MAX_PAYLOAD_BYTES, fixture.maxPayloadBytes);
});

for (const vector of fixture.vectors) {
  test(`golden frame ${vector.name}`, async (t) => {
    const room = `golden-${vector.name}`;
    const first = await RawWebSocket.connect({ token, room });
    const second = await RawWebSocket.connect({ token, room });
    t.after(() => first.close());
    t.after(() => second.close());
    await sendHello(first, "network-a");
    await sendHello(second, "network-b");

    const frame = Buffer.from(vector.frameBase64, "base64");
    first.sendBinary(frame);
    if (!vector.valid || vector.name === "hello") {
      // hello is a valid admission/control frame, not a peer-relayed request.
      await assertNoBinary(second);
      return;
    }
    assert.deepEqual(await second.nextBinary(), frame);
  });
}

test("relay enforces the golden payload boundary", async (t) => {
  const first = await RawWebSocket.connect({ token, room: "golden-payload-boundary" });
  const second = await RawWebSocket.connect({ token, room: "golden-payload-boundary" });
  t.after(() => first.close());
  t.after(() => second.close());
  await sendHello(first, "network-a");
  await sendHello(second, "network-b");

  const header = Buffer.from('{"t":"req","id":"payload-boundary"}\n');
  const atCap = Buffer.concat([header, Buffer.alloc(fixture.maxPayloadBytes, 0x61)]);
  first.sendBinary(atCap);
  assert.deepEqual(await second.nextBinary(10_000), atCap);

  const overCap = Buffer.concat([header, Buffer.alloc(fixture.maxPayloadBytes + 1, 0x62)]);
  first.sendBinary(overCap);
  await assertNoBinary(second, 800);
});

async function sendHello(socket, network) {
  socket.sendBinary(Buffer.from(JSON.stringify({ t: "hello", network })));
  await new Promise((resolve) => setTimeout(resolve, 20));
}

async function assertNoBinary(socket, timeoutMs = 300) {
  await assert.rejects(socket.nextBinary(timeoutMs), /timed out waiting for binary frame/);
}

async function waitForWorker() {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (worker.exitCode !== null) throw new Error("wrangler dev exited before becoming ready");
    try {
      if ((await fetch(`http://${host}:${port}/`)).status === 404) return;
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
      if (typeof address !== "object" || address === null) return reject(new Error("no port"));
      listener.close((error) => (error ? reject(error) : resolve(address.port)));
    });
  });
}

class RawWebSocket {
  #socket;
  #buffer = Buffer.alloc(0);
  #events = [];
  #waiters = [];

  static async connect({ token: bearerToken, room }) {
    const socket = net.connect(port, host);
    await once(socket, "connect");
    const key = randomBytes(16).toString("base64");
    socket.write([
      `GET /parties/relay-room/${room} HTTP/1.1`, `Host: ${host}:${port}`, "Upgrade: websocket",
      "Connection: Upgrade", `Sec-WebSocket-Key: ${key}`, "Sec-WebSocket-Version: 13",
      `Authorization: Bearer ${bearerToken}`, "", ""
    ].join("\r\n"));
    const response = await readUpgradeResponse(socket);
    if (!response.startsWith("HTTP/1.1 101")) {
      socket.destroy();
      throw new Error(response.split("\r\n", 1)[0]);
    }
    const accept = response.match(/sec-websocket-accept:\s*(.+)\r?$/im)?.[1]?.trim();
    const expected = createHash("sha1").update(`${key}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`).digest("base64");
    assert.equal(accept, expected, "server accepted the WebSocket handshake");
    return new RawWebSocket(socket);
  }

  constructor(socket) {
    this.#socket = socket;
    socket.on("data", (chunk) => this.#receive(chunk));
    socket.on("close", () => this.#emit({ type: "close", code: 1006 }));
    socket.on("error", (error) => this.#emit({ type: "error", error }));
  }

  sendBinary(data) {
    const body = Buffer.from(data);
    const mask = randomBytes(4);
    const header = websocketFrameHeader(0x82, body.length, true);
    for (let index = 0; index < body.length; index += 1) body[index] ^= mask[index % 4];
    this.#socket.write(Buffer.concat([header, mask, body]));
  }

  nextBinary(timeoutMs = 3000) {
    return this.#next("binary", timeoutMs).then((event) => event.data);
  }

  close() {
    if (!this.#socket.destroyed) this.#socket.end(Buffer.from([0x88, 0x80, 0, 0, 0, 0]));
  }

  #next(type, timeoutMs) {
    const existing = this.#events.findIndex((event) => event.type === type);
    if (existing >= 0) return Promise.resolve(this.#events.splice(existing, 1)[0]);
    return new Promise((resolve, reject) => {
      const waiter = { type, resolve, reject, timeout: undefined };
      waiter.timeout = setTimeout(() => {
        const index = this.#waiters.indexOf(waiter);
        if (index >= 0) this.#waiters.splice(index, 1);
        reject(new Error(`timed out waiting for ${type} frame after ${timeoutMs}ms`));
      }, timeoutMs);
      this.#waiters.push(waiter);
    });
  }

  #emit(event) {
    const index = this.#waiters.findIndex((waiter) => waiter.type === event.type);
    if (index >= 0) {
      const waiter = this.#waiters.splice(index, 1)[0];
      clearTimeout(waiter.timeout);
      waiter.resolve(event);
    } else this.#events.push(event);
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
      } else if (length === 127) {
        if (this.#buffer.length < 10) return;
        length = Number(this.#buffer.readBigUInt64BE(2));
        offset = 10;
      }
      if (this.#buffer.length < offset + length) return;
      const payload = this.#buffer.subarray(offset, offset + length);
      this.#buffer = this.#buffer.subarray(offset + length);
      if ((first & 0x0f) === 0x2) this.#emit({ type: "binary", data: Buffer.from(payload) });
      if ((first & 0x0f) === 0x8) this.#emit({ type: "close", code: payload.readUInt16BE() });
    }
  }
}

function websocketFrameHeader(first, length, masked) {
  const flag = masked ? 0x80 : 0;
  if (length < 126) return Buffer.from([first, flag | length]);
  if (length <= 0xffff) return Buffer.from([first, flag | 126, length >> 8, length & 0xff]);
  const header = Buffer.alloc(10);
  header[0] = first;
  header[1] = flag | 127;
  header.writeBigUInt64BE(BigInt(length), 2);
  return header;
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
