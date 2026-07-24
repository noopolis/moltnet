import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";

const [artifactPath] = process.argv.slice(2);
if (!artifactPath) throw new Error("usage: verify-machine-contract.mjs ARTIFACT");
const bytes = await readFile(artifactPath);
if (bytes.at(-1) === 0x0a) throw new Error("artifact must be one canonical JSON value without LF");
const contract = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
if (contract.version !== "moltnet.machine-contract.v1" || contract.framing?.encoding !== "utf-8-json"
  || contract.framing?.record_separator !== "\n" || contract.framing?.strict_json !== true) {
  throw new Error("invalid declared framing");
}
const shapes = new Map(contract.shapes.map((shape) => [shape.name, shape]));
const limits = contract.limits;
const grammar = contract.grammars ?? {};
const bytesOf = (value) => Buffer.byteLength(value, "utf8");
const limit = (name) => {
  const value = Object.hasOwn(limits, name) ? limits[name] : /^\d+$/u.test(name) ? Number(name) : Number.NaN;
  if (!Number.isSafeInteger(value) || value < 0) throw new Error(`unknown limit ${name}`);
  return value;
};
const present = (value) => value !== undefined && value !== null;
const asObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);

function checkLimit(value, spec) {
  if (!spec || spec.includes("..") || spec.includes("|")) return;
  if (bytesOf(value) > limit(spec)) throw new Error(`limit ${spec}`);
}
function checkString(value, field) {
  if (typeof value !== "string") throw new Error(`string ${field.name}`);
  checkLimit(value, field.limit);
  if (field.type === "non_blank_string" && value.trim() === "") throw new Error(`blank ${field.name}`);
  if (["identifier", "room_id", "message_id"].includes(field.type)) {
    const local = grammar.local_identifier;
    if (!new RegExp(local.pattern).test(value) || bytesOf(value) < local.min_bytes || bytesOf(value) > local.max_bytes) throw new Error(`identifier ${field.name}`);
  }
  if (field.type === "lowercase_sha256" && !new RegExp(grammar.lowercase_sha256.pattern).test(value)) throw new Error(`sha ${field.name}`);
  if (field.type === "rfc3339_timestamp" && Number.isNaN(Date.parse(value))) throw new Error(`timestamp ${field.name}`);
  if (field.type === "http|https|molt_url") {
    const url = new URL(value); if (!["http:", "https:", "molt:"].includes(url.protocol)) throw new Error(`url ${field.name}`);
  }
  if (field.enum?.length && !field.enum.includes(value)) throw new Error(`enum ${field.name}`);
}
function checkArray(value, field) {
  if (!Array.isArray(value)) throw new Error(`array ${field.name}`);
  const [count, item] = (field.limit ?? "").split("|");
  if (count && !count.includes("..") && value.length > limit(count)) throw new Error(`array limit ${field.name}`);
  const itemShape = shapes.get(field.type.replace(/_array$/, ""));
  const seen = new Set();
  for (const entry of value) {
    if (itemShape) checkShape(entry, itemShape);
    else if (typeof entry !== "string") throw new Error(`array item ${field.name}`);
    else {
      if (item && bytesOf(entry) > limit(item)) throw new Error(`array item limit ${field.name}`);
      if (field.type === "unique_identifier_array") {
        checkString(entry, { name: field.name, type: "identifier", limit: item });
        if (seen.has(entry)) throw new Error(`duplicate ${field.name}`); seen.add(entry);
      }
    }
  }
}
function checkField(value, field) {
  const nested = shapes.get(field.type);
  if (nested) return checkShape(value, nested);
  if (field.type === "boolean") { if (typeof value !== "boolean") throw new Error(`boolean ${field.name}`); return; }
  if (field.type === "integer") { if (!Number.isInteger(value)) throw new Error(`integer ${field.name}`); const max = (field.limit ?? "").replace(/^1\.\./, ""); if (field.limit?.startsWith("1..") && (value < 1 || value > limit(max))) throw new Error(`integer limit ${field.name}`); return; }
  if (field.type === "json_object") { if (!asObject(value)) throw new Error(`object ${field.name}`); return; }
  if (field.type === "json_value") return;
  if (field.type.endsWith("_array")) return checkArray(value, field);
  checkString(value, field);
}
function checkRelations(value, relations = []) {
  for (const relation of relations) {
    const fields = relation.fields ?? [];
    switch (relation.kind) {
      case "exactly_one":
        if (fields.filter((field) => present(value[field])).length !== 1) throw new Error("exactly_one");
        break;
      case "mutually_exclusive":
        if (fields.filter((field) => present(value[field])).length > 1) throw new Error("mutually_exclusive");
        break;
      case "payload_key_equals_field":
      case "result_key_equals_field": {
        if (fields.length !== 1) throw new Error(relation.kind);
        if (relation.kind === "result_key_equals_field" && (present(value.error) || present(value.event))) break;
        if (!present(value[value[fields[0]]])) throw new Error(relation.kind);
        break;
      }
      case "field_allowed_when":
        if (fields.length !== 2 || (present(value[fields[0]]) && value[fields[1]] !== relation.value)) throw new Error("field_allowed_when");
        break;
      case "present_iff_true":
        if (fields.length !== 2 || typeof value[fields[1]] !== "boolean" || present(value[fields[0]]) !== value[fields[1]]) throw new Error("present_iff_true");
        break;
      case "exactly_one_when_true":
        if (fields.length !== 3 || typeof value[fields[2]] !== "boolean"
          || (value[fields[2]] && fields.slice(0, 2).filter((field) => present(value[field])).length !== 1)) {
          throw new Error("exactly_one_when_true");
        }
        break;
      case "absent_when_false":
        if (fields.length !== 3 || typeof value[fields[2]] !== "boolean"
          || (!value[fields[2]] && fields.slice(0, 2).some((field) => present(value[field])))) {
          throw new Error("absent_when_false");
        }
        break;
      case "at_least_one_nonempty":
        if (!fields.some((field) => Array.isArray(value[field]) && value[field].length)) throw new Error("at_least_one_nonempty");
        break;
      case "sha256_utf8_matches": {
        if (fields.length !== 2 || typeof value[fields[0]] !== "string" || typeof value[fields[1]] !== "string") {
          throw new Error("sha256_utf8_matches");
        }
        const digest = createHash("sha256").update(Buffer.from(value[fields[0]], "utf8")).digest("hex");
        if (digest !== value[fields[1]]) throw new Error("sha256_utf8_matches");
        break;
      }
      case "kind_requires_only":
        if (fields.length !== 2 || typeof value[fields[0]] !== "string"
          || (value[fields[0]] === relation.value && !present(value[fields[1]]))) {
          throw new Error("kind_requires_only");
        }
        break;
      default:
        throw new Error(`unknown relation ${relation.kind}`);
    }
  }
}
function checkShape(value, shape) {
  if (!asObject(value)) throw new Error(`shape ${shape.name}`);
  const fields = new Map(shape.fields.map((field) => [field.name, field]));
  for (const key of Object.keys(value)) if (!fields.has(key)) throw new Error(`unknown ${shape.name}.${key}`);
  for (const field of shape.fields) {
    if (!present(value[field.name])) { if (field.required && !field.nullable) throw new Error(`missing ${shape.name}.${field.name}`); continue; }
    checkField(value[field.name], field);
  }
  checkRelations(value, shape.relations);
}

for (const operation of contract.operations) if (!contract.enums.operation.includes(operation)) throw new Error("operation drift");
for (const vector of contract.vectors) {
  if (vector.line.includes("\n") || JSON.stringify(JSON.parse(vector.line)) !== vector.line) throw new Error(`noncanonical ${vector.name}`);
  const digest = createHash("sha256").update(Buffer.from(vector.line, "utf8")).digest("hex");
  if (digest !== vector.sha256) throw new Error(`hash ${vector.name}`);
  checkShape(JSON.parse(vector.line), shapes.get(vector.direction));
}
process.stdout.write(`verified ${contract.vectors.length} vectors\n`);
