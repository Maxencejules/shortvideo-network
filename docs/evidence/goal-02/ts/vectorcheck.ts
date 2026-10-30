// Vector checker for SVN-JCS-1 and uint64 arithmetic.
// The Ed25519 seed is the public RFC 8032 test vector.
// RFC8032_TEST_VECTOR_NOT_A_SECRET. Do not use it outside these fixtures.

import crypto from "node:crypto";
import fs from "node:fs";

const SAFE_MAX = 9007199254740991n;
const UINT64_MAX = (1n << 64n) - 1n;
const MAX_DEPTH = 8;

type Json = null | boolean | bigint | string | Json[] | { [key: string]: Json };

class JCSError extends Error {
  reason: string;
  constructor(reason: string) {
    super(reason);
    this.reason = reason;
  }
}

interface ValidVec {
  id: string;
  input_raw: string;
  canonical: string;
}
interface InvalidVec {
  id: string;
  raw: string;
  reason: string;
}
interface IntVec {
  id: string;
  decimal: string;
  safe: boolean;
  uint64: boolean;
}
interface ArithVec {
  id: string;
  op: string;
  a: string;
  b: string;
  result: string;
}
interface SignVec {
  id: string;
  domain: string;
  vector_id: string;
}
interface Vectors {
  valid: ValidVec[];
  invalid: InvalidVec[];
  integers: IntVec[];
  arithmetic: ArithVec[];
  rfc8032_empty_signature: string;
  rfc8032_seed: string;
  sign: SignVec[];
}

function canonicalize(value: Json): string {
  const text = emit(value, 0);
  if (Buffer.byteLength(text, "utf8") > 65536) {
    throw new JCSError("bounds");
  }
  return text;
}

function emit(value: Json, depth: number): string {
  if (depth > MAX_DEPTH) {
    throw new JCSError("depth");
  }
  if (value === null) {
    throw new JCSError("null");
  }
  if (typeof value === "boolean") {
    return value ? "true" : "false";
  }
  if (typeof value === "bigint") {
    if (value > SAFE_MAX || value < -SAFE_MAX) {
      throw new JCSError("unsafe_integer");
    }
    return value.toString();
  }
  if (typeof value === "string") {
    return emitString(value);
  }
  if (Array.isArray(value)) {
    return "[" + value.map((item) => emit(item, depth + 1)).join(",") + "]";
  }
  const keys = Object.keys(value);
  for (const key of keys) {
    checkKey(key);
  }
  keys.sort(compareUTF16);
  return "{" + keys.map((key) => emitString(key) + ":" + emit(value[key], depth + 1)).join(",") + "}";
}

function emitString(text: string): string {
  let out = '"';
  for (let index = 0; index < text.length; ) {
    const code = text.codePointAt(index) as number;
    const width = code > 0xffff ? 2 : 1;
    if (code === 0x22) out += '\\"';
    else if (code === 0x5c) out += "\\\\";
    else if (code === 0x08) out += "\\b";
    else if (code === 0x09) out += "\\t";
    else if (code === 0x0a) out += "\\n";
    else if (code === 0x0c) out += "\\f";
    else if (code === 0x0d) out += "\\r";
    else if (code < 0x20) out += "\\u" + code.toString(16).padStart(4, "0");
    else out += String.fromCodePoint(code);
    index += width;
  }
  return out + '"';
}

function compareUTF16(a: string, b: string): number {
  const length = Math.min(a.length, b.length);
  for (let index = 0; index < length; index += 1) {
    const delta = a.charCodeAt(index) - b.charCodeAt(index);
    if (delta !== 0) {
      return delta;
    }
  }
  return a.length - b.length;
}

function checkKey(key: string): void {
  if (!/^[A-Za-z0-9._-]{1,64}$/.test(key)) {
    throw new JCSError("bad_key");
  }
}

function skip(raw: string, index: number): number {
  while (index < raw.length && " \t\r\n".includes(raw[index])) {
    index += 1;
  }
  return index;
}

function parseValue(raw: string, index: number, depth: number): [Json, number] {
  if (depth > MAX_DEPTH) {
    throw new JCSError("depth");
  }
  index = skip(raw, index);
  if (index >= raw.length) {
    throw new JCSError("malformed");
  }
  const char = raw[index];
  if (char === "{") return parseObject(raw, index, depth);
  if (char === "[") return parseArray(raw, index, depth);
  if (char === '"') return parseString(raw, index);
  if (char === "t") return literal(raw, index, "true", true);
  if (char === "f") return literal(raw, index, "false", false);
  if (char === "n") throw new JCSError("null");
  if (char === "-" || (char >= "0" && char <= "9")) return parseNumber(raw, index);
  throw new JCSError("malformed");
}

function literal(raw: string, index: number, text: string, value: boolean): [Json, number] {
  if (raw.slice(index, index + text.length) !== text) {
    throw new JCSError("unknown_literal");
  }
  return [value, index + text.length];
}

function parseNumber(raw: string, index: number): [Json, number] {
  const start = index;
  if (raw[index] === "-") {
    index += 1;
    if (index >= raw.length || raw[index] < "0" || raw[index] > "9") {
      throw new JCSError("malformed");
    }
  }
  if (raw[index] === "0") {
    index += 1;
    if (index < raw.length && raw[index] >= "0" && raw[index] <= "9") {
      throw new JCSError("leading_zero");
    }
  } else {
    if (index >= raw.length || raw[index] < "0" || raw[index] > "9") {
      throw new JCSError("malformed");
    }
    while (index < raw.length && raw[index] >= "0" && raw[index] <= "9") {
      index += 1;
    }
  }
  if (index < raw.length && (raw[index] === "." || raw[index] === "e" || raw[index] === "E")) {
    throw new JCSError(raw[index] === "." ? "fraction" : "exponent");
  }
  const token = raw.slice(start, index);
  if (token === "-0" || token === "-") {
    throw new JCSError("negative_zero");
  }
  const value = BigInt(token);
  if (value > SAFE_MAX || value < -SAFE_MAX) {
    throw new JCSError("unsafe_integer");
  }
  return [value, index];
}

function parseString(raw: string, index: number): [Json, number] {
  if (raw[index] !== '"') {
    throw new JCSError("malformed");
  }
  index += 1;
  let text = "";
  while (index < raw.length) {
    const char = raw[index];
    if (char === '"') {
      return [text, index + 1];
    }
    if (char === "\\") {
      index += 1;
      if (index >= raw.length) {
        throw new JCSError("malformed");
      }
      const escaped = raw[index];
      const mapping: { [key: string]: string } = {
        b: "\b",
        t: "\t",
        n: "\n",
        f: "\f",
        r: "\r",
        '"': '"',
        "\\": "\\",
        "/": "/",
      };
      if (escaped in mapping) {
        text += mapping[escaped];
        index += 1;
        continue;
      }
      if (escaped !== "u") {
        throw new JCSError("malformed");
      }
      const hexDigits = raw.slice(index + 1, index + 5);
      if (!/^[0-9a-fA-F]{4}$/.test(hexDigits)) {
        throw new JCSError("malformed");
      }
      let code = Number.parseInt(hexDigits, 16);
      index += 5;
      if (code >= 0xd800 && code <= 0xdbff) {
        if (raw.slice(index, index + 2) !== "\\u") {
          throw new JCSError("lone_surrogate");
        }
        const lowHex = raw.slice(index + 2, index + 6);
        const low = Number.parseInt(lowHex, 16);
        if (!/^[0-9a-fA-F]{4}$/.test(lowHex) || low < 0xdc00 || low > 0xdfff) {
          throw new JCSError("lone_surrogate");
        }
        code = 0x10000 + ((code - 0xd800) << 10) + (low - 0xdc00);
        index += 6;
      } else if (code >= 0xdc00 && code <= 0xdfff) {
        throw new JCSError("lone_surrogate");
      }
      text += String.fromCodePoint(code);
      continue;
    }
    if (char.charCodeAt(0) < 0x20) {
      throw new JCSError("malformed");
    }
    const code = raw.codePointAt(index) as number;
    text += String.fromCodePoint(code);
    index += code > 0xffff ? 2 : 1;
  }
  throw new JCSError("malformed");
}

function parseObject(raw: string, index: number, depth: number): [Json, number] {
  index += 1;
  const obj: { [key: string]: Json } = {};
  index = skip(raw, index);
  if (raw[index] === "}") {
    return [obj, index + 1];
  }
  while (index < raw.length) {
    index = skip(raw, index);
    const [keyValue, afterKey] = parseString(raw, index);
    const key = keyValue as string;
    if (Object.prototype.hasOwnProperty.call(obj, key)) {
      throw new JCSError("duplicate_key");
    }
    checkKey(key);
    index = skip(raw, afterKey);
    if (raw[index] !== ":") {
      throw new JCSError("malformed");
    }
    const [value, afterValue] = parseValue(raw, index + 1, depth + 1);
    obj[key] = value;
    index = skip(raw, afterValue);
    if (raw[index] === "}") {
      return [obj, index + 1];
    }
    if (raw[index] !== ",") {
      throw new JCSError("malformed");
    }
    index += 1;
    index = skip(raw, index);
    if (raw[index] === "}") {
      throw new JCSError("trailing_comma");
    }
  }
  throw new JCSError("malformed");
}

function parseArray(raw: string, index: number, depth: number): [Json, number] {
  index += 1;
  const items: Json[] = [];
  index = skip(raw, index);
  if (raw[index] === "]") {
    return [items, index + 1];
  }
  while (index < raw.length) {
    const [value, after] = parseValue(raw, index, depth + 1);
    items.push(value);
    index = skip(raw, after);
    if (raw[index] === "]") {
      return [items, index + 1];
    }
    if (raw[index] !== ",") {
      throw new JCSError("malformed");
    }
    index += 1;
    index = skip(raw, index);
    if (raw[index] === "]") {
      throw new JCSError("trailing_comma");
    }
  }
  throw new JCSError("malformed");
}

function parseAndCanonicalize(raw: string): string {
  const [value, index] = parseValue(raw, 0, 0);
  const end = skip(raw, index);
  if (end !== raw.length) {
    throw new JCSError("trailing");
  }
  return canonicalize(value);
}

function classifyDecimal(decimal: string): [boolean, boolean] {
  if (!/^-?(0|[1-9][0-9]*)$/.test(decimal)) {
    return [false, false];
  }
  const value = BigInt(decimal);
  const abs = value < 0n ? -value : value;
  const safe = abs <= SAFE_MAX;
  return [safe, value >= 0n && value <= UINT64_MAX];
}

function applyArith(op: string, a: string, b: string): string {
  if (!/^(0|[1-9][0-9]*)$/.test(a) || !/^(0|[1-9][0-9]*)$/.test(b)) {
    return "malformed";
  }
  const left = BigInt(a);
  const right = BigInt(b);
  if (left > UINT64_MAX || right > UINT64_MAX) {
    return "malformed";
  }
  if (op === "add") {
    if (left > UINT64_MAX - right) {
      return "overflow";
    }
    return (left + right).toString();
  }
  if (op === "sub") {
    if (left < right) {
      return "underflow";
    }
    return (left - right).toString();
  }
  return "malformed";
}

function privateKey(seedHex: string): crypto.KeyObject {
  const seed = Buffer.from(seedHex, "hex");
  const pkcs8 = Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), seed]);
  return crypto.createPrivateKey({ key: pkcs8, format: "der", type: "pkcs8" });
}

function main(): void {
  const path = process.argv[2];
  if (!path) {
    throw new Error("usage: vectorcheck vectors.json");
  }
  const doc = JSON.parse(fs.readFileSync(path, "utf8")) as Vectors;
  const canonicalByID = new Map<string, string>();
  for (const item of doc.valid) {
    const got = parseAndCanonicalize(item.input_raw);
    if (got !== item.canonical) {
      throw new Error(`VALID ${item.id} MISMATCH`);
    }
    const sum = crypto.createHash("sha256").update(got, "utf8").digest("hex");
    process.stdout.write(`VALID ${item.id} ${sum}\n`);
    canonicalByID.set(item.id, got);
  }
  for (const item of doc.invalid) {
    try {
      parseAndCanonicalize(item.raw);
      throw new Error(`INVALID ${item.id} MISSED`);
    } catch (error) {
      if (!(error instanceof JCSError) || error.reason !== item.reason) {
        throw error;
      }
    }
    process.stdout.write(`INVALID ${item.id} ${item.reason}\n`);
  }
  for (const item of doc.integers) {
    const [safe, fits] = classifyDecimal(item.decimal);
    if (safe !== item.safe || fits !== item.uint64) {
      throw new Error(`INTEGER ${item.id} MISMATCH`);
    }
    process.stdout.write(`INTEGER ${item.id} ${safe ? "safe" : "unsafe"} ${fits ? "uint64" : "over"}\n`);
  }
  for (const item of doc.arithmetic) {
    const got = applyArith(item.op, item.a, item.b);
    if (got !== item.result) {
      throw new Error(`ARITH ${item.id} MISMATCH ${got}`);
    }
    process.stdout.write(`ARITH ${item.id} ${got}\n`);
  }
  const key = privateKey(doc.rfc8032_seed);
  const empty = crypto.sign(null, Buffer.alloc(0), key).toString("hex");
  if (empty !== doc.rfc8032_empty_signature) {
    throw new Error("RFC8032 MISMATCH");
  }
  process.stdout.write(`RFC8032 ${empty}\n`);
  for (const item of doc.sign) {
    const canon = canonicalByID.get(item.vector_id);
    if (canon === undefined) {
      throw new Error(`SIGN ${item.id} MISSING`);
    }
    const input = Buffer.concat([Buffer.from(item.domain, "utf8"), Buffer.from([0]), Buffer.from(canon, "utf8")]);
    const sum = crypto.createHash("sha256").update(input).digest("hex");
    const signature = crypto.sign(null, input, key).toString("hex");
    process.stdout.write(`SIGN ${item.id} ${sum} ${signature}\n`);
  }
}

main();
