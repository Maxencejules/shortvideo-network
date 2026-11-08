// Evidence label catalog checks (goal 09). The same rules run in Go in
// agents/auditor/labels/labels_test.go against the embedded catalog. This
// file runs through scripts/run_tests.py with --experimental-strip-types;
// tsc does not type-check it (apps/web/tsconfig.json includes only src).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import * as labelsModule from "../src/evidence-labels.ts";
import { EVIDENCE_LABELS } from "../src/evidence-labels.ts";

const ROOT = new URL("../../../", import.meta.url);
const CATALOG_PATH = "agents/auditor/labels/evidence-labels.json";
const RULES_PATH = "agents/auditor/labels/testdata/label-rules.json";
const SCHEMA_PATH = "docs/protocol/schemas/evidence-envelope.schema.json";

type Rules = {
  fixture: string;
  identifiers: { catalog: string; claim_limit: string };
  denylist: string[];
  negation_allowlist: string[];
};

// Every catalog string, sentence by sentence, in file order. Written here,
// independently of the Go test's own list, so that a catalog change fails
// both tests until both lists are edited.
const PINNED: [string, string[]][] = [
  ["modes.AUDITOR_ATTESTATION.label", ["Auditor attestation"]],
  ["modes.AUDITOR_ATTESTATION.text", ["An admitted auditor signed this statement about one lease.", "It is an attestation.", "It is not proof of storage."]],
  ["modes.PROVIDER_RECEIPT.label", ["Provider receipt"]],
  ["modes.PROVIDER_RECEIPT.text", ["A transfer receipt attributed to a provider.", "This beta does not check its signature and does not record it on chain.", "It is an attestation.", "It is not proof of storage or of future delivery."]],
  ["states.RECORDED.label", ["Recorded"]],
  ["states.RECORDED.text", ["The auditor reported the lease's manifest digest.", "The chain compared two digests and fetched nothing.", "The manifest digest is public.", "Reporting it does not show that any byte was fetched.", "A recorded attestation describes one past observation.", "It does not show that the lease is held or the content is available now."]],
  ["states.REJECTED.label", ["Rejected"]],
  ["states.REJECTED.text", ["The auditor reported a digest that differs from the lease's manifest digest.", "This can mean a failed or mismatched check.", "The chain did not check why.", "A rejected record does not expire.", "The chain cannot tell whether the provider or the auditor caused the difference."]],
  ["states.EXPIRED.label", ["Expired"]],
  ["states.EXPIRED.text", ["The attestation's expiry time has passed."]],
  ["event_accepted", ["EvidenceAccepted means the record was written to the ledger.", "It does not mean the check passed."]],
  ["acceptance.label", ["Provider acceptance"]],
  ["acceptance.text", ["The provider signed the package digest, byte count and terms.", "Acceptance is a promise, not possession."]],
  ["limitations.0", ["Auditors can lie."]],
  ["limitations.1", ["Providers may fetch from another host."]],
  ["limitations.2", ["Node IDs do not prove independent replicas."]],
  ["limitations.3", ["A valid signature does not make an observation true."]],
  ["limitations.4", ["Accounting success is not physical capacity or operator independence."]],
  ["limitations.5", ["A sampled check covers only the sampled segments."]],
  ["limitations.6", ["This auditor is not shown to be independent of the provider."]],
  ["limitations.7", ["No evidence record renews, extends or revives a lease, in any state."]],
  ["limitations.8", ["No cryptographic storage proof exists in this beta.", "It is a separate decision gate."]],
];

// The full list of denied phrases; the shared rules file must hold exactly
// these, so removing one there fails this test.
const DENYLIST = [
  "proof of storage", "storage proof", "proof-of-storage", "verified storage", "verified copy",
  "verified replica", "unique replica", "independent copies", "independently operated", "trustless",
  "guarantee", "lawful", "legal", "certified", "audited network", "independent", "verified", "proven",
  "future delivery", "quality", "independence", "legality", "illegal", "unlawful", "future quality",
  "merkle", "inclusion proof", "unique", "uniquely stored", "proof", "prove", "proof of possession",
  "proof of replication", "proof of retrievability", "filecoin", "reward", "slash", "verif", "certif",
  "confirm", "audited", "honest", "truthful", "accurate", "authentic", "trusted", "trustworthy", "true",
  "replica", "replicat", "redundan", "durab", "permanent", "immutable", "available", "availability",
  "retrievab", "possess", "stored", "stores", "renew", "extend", "reviv", "production", "secure",
];

function readText(path: string): string {
  return readFileSync(new URL(path, ROOT), "utf8");
}

function loadRules(): Rules {
  const rules = JSON.parse(readText(RULES_PATH)) as Rules;
  assert.equal(rules.fixture, "svn-evidence-label-rules-v0");
  assert.deepEqual(Object.keys(rules), ["fixture", "identifiers", "denylist", "negation_allowlist"]);
  return rules;
}

// Split a text after each ". "; joining the parts with one space gives it back.
function sentences(text: string): string[] {
  const parts = text.split(". ");
  return parts.map((part, i) => (i < parts.length - 1 ? part + "." : part));
}

// Every string leaf of a value, by dotted path, in key order.
function leaves(value: unknown, path = ""): [string, string][] {
  const join = (key: string): string => (path === "" ? key : path + "." + key);
  if (typeof value === "string") {
    return [[path, value]];
  }
  if (Array.isArray(value)) {
    return value.flatMap((child, i) => leaves(child, join(String(i))));
  }
  if (value !== null && typeof value === "object") {
    return Object.entries(value).flatMap(([key, child]) => leaves(child, join(key)));
  }
  throw new Error(`${path}: ${typeof value} is not a string`);
}

// Every label, text, and limitation: all string leaves but the two identifiers.
function humanTexts(): [string, string][] {
  return leaves(EVIDENCE_LABELS).filter(([path]) => path !== "catalog" && path !== "claim_limit");
}

// The denied phrases in one sentence (the Go test's rule): the sentence is
// normalized first, an underscore to a space and a space at each camel-case
// boundary, so STORAGE_PROOF and storageProof read as "storage proof"; a
// phrase matches case-insensitively where it starts at an ASCII word boundary.
function deniedIn(denylist: string[], sentence: string): string[] {
  const normalized = sentence.replace(/_/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2");
  return denylist.filter((phrase) => new RegExp("\\b" + phrase, "i").test(normalized));
}

function violations(rules: Rules, text: string): string[] {
  return sentences(text)
    .filter((sentence) => !rules.negation_allowlist.includes(sentence))
    .flatMap((sentence) => deniedIn(rules.denylist, sentence).map((phrase) => `${JSON.stringify(sentence)} contains ${JSON.stringify(phrase)}`));
}

const NEGATION = /\b(no|not|cannot|never)\b/i;

test("the module exports only the catalog, and its JSON text is the Go catalog file byte for byte", () => {
  assert.deepEqual(Object.keys(labelsModule), ["EVIDENCE_LABELS"]);
  const file = readText(CATALOG_PATH);
  assert.deepStrictEqual(EVIDENCE_LABELS, JSON.parse(file));
  assert.equal(JSON.stringify(EVIDENCE_LABELS, null, 2) + "\n", file);
  assert.equal(JSON.stringify(JSON.parse(file), null, 2) + "\n", file, "the catalog file is not canonical");
});

test("every catalog string is pinned sentence by sentence, in order", () => {
  const rules = loadRules();
  assert.equal(EVIDENCE_LABELS.catalog, rules.identifiers.catalog);
  assert.equal(EVIDENCE_LABELS.claim_limit, rules.identifiers.claim_limit);
  const texts = humanTexts();
  assert.deepEqual(
    texts.map(([path]) => path),
    PINNED.map(([path]) => path),
  );
  for (const [i, [path, text]] of texts.entries()) {
    assert.deepEqual(sentences(text), PINNED[i][1], path);
    assert.equal(PINNED[i][1].join(" "), text, path);
  }
});

test("every string is bounded printable ASCII; labels are phrases, texts are sentences", () => {
  for (const [path, text] of humanTexts()) {
    assert.match(text, /^[\x20-\x7e]{1,512}$/, path);
    assert.doesNotMatch(text, /^ | $| {2}/, path);
    if (path.endsWith(".label")) {
      assert.doesNotMatch(text, /\./, path);
      continue;
    }
    for (const s of sentences(text)) {
      assert.match(s, /^[A-Z][^.]*[^. ]\.$/, `${path}: ${s}`);
    }
  }
});

test("the mode is explicit: auditor attestation or provider receipt (goal requirement 4)", () => {
  assert.deepEqual(Object.keys(EVIDENCE_LABELS.modes).sort(), ["AUDITOR_ATTESTATION", "PROVIDER_RECEIPT"]);
  assert.equal(EVIDENCE_LABELS.modes.AUDITOR_ATTESTATION.label, "Auditor attestation");
  assert.equal(EVIDENCE_LABELS.modes.PROVIDER_RECEIPT.label, "Provider receipt");
  for (const mode of Object.values(EVIDENCE_LABELS.modes)) {
    assert.ok(sentences(mode.text).includes("It is an attestation."), mode.label);
  }
  assert.ok(
    sentences(EVIDENCE_LABELS.modes.PROVIDER_RECEIPT.text).includes(
      "This beta does not check its signature and does not record it on chain.",
    ),
  );
});

test("the state keys are the three short reported state names", () => {
  assert.deepEqual(Object.keys(EVIDENCE_LABELS.states).sort(), ["EXPIRED", "RECORDED", "REJECTED"]);
});

test("the goal limitations, REQ-INV-007, and the decision gate appear verbatim", () => {
  const limitations: readonly string[] = EVIDENCE_LABELS.limitations;
  for (const want of [
    "Auditors can lie.",
    "Providers may fetch from another host.",
    "Node IDs do not prove independent replicas.",
    "Accounting success is not physical capacity or operator independence.",
    "A valid signature does not make an observation true.",
    "No cryptographic storage proof exists in this beta. It is a separate decision gate.",
  ]) {
    assert.ok(limitations.includes(want), want);
  }
  assert.equal(new Set(limitations).size, limitations.length, "a limitation repeats");
});

test("the acceptance text calls the signature a promise, not possession (REQ-EVID-002)", () => {
  assert.equal(EVIDENCE_LABELS.acceptance.label, "Provider acceptance");
  const got = sentences(EVIDENCE_LABELS.acceptance.text);
  assert.ok(got.includes("The provider signed the package digest, byte count and terms."));
  assert.ok(got.includes("Acceptance is a promise, not possession."));
});

test("state and event texts limit the claim: past observation, public digest, no blame, not a pass", () => {
  const cases: [string, string, string[]][] = [
    ["RECORDED", EVIDENCE_LABELS.states.RECORDED.text, [
      "The chain compared two digests and fetched nothing.",
      "The manifest digest is public.",
      "Reporting it does not show that any byte was fetched.",
      "A recorded attestation describes one past observation.",
      "It does not show that the lease is held or the content is available now.",
    ]],
    ["REJECTED", EVIDENCE_LABELS.states.REJECTED.text, [
      "The chain did not check why.",
      "A rejected record does not expire.",
      "The chain cannot tell whether the provider or the auditor caused the difference.",
    ]],
    ["event_accepted", EVIDENCE_LABELS.event_accepted, ["It does not mean the check passed."]],
  ];
  for (const [name, text, wants] of cases) {
    for (const want of wants) {
      assert.ok(sentences(text).includes(want), `${name} lacks ${want}`);
    }
  }
});

test("the no-renewal sentence is catalog-wide; no mode, state, acceptance, or event text renews, extends, revives, or prolongs", () => {
  const renewal = "No evidence record renews, extends or revives a lease, in any state.";
  const limitations: readonly string[] = EVIDENCE_LABELS.limitations;
  assert.ok(limitations.includes(renewal));
  for (const [path, text] of humanTexts()) {
    if (path.startsWith("limitations.")) {
      continue;
    }
    assert.doesNotMatch(text, /\b(renew|extend|reviv|prolong)/i, path);
  }
});

test("claim_limit and the modes match the frozen envelope schema", () => {
  const schema = JSON.parse(readText(SCHEMA_PATH)) as {
    properties: { claim_limit: { const: string }; kind: { enum: string[] } };
  };
  assert.equal(EVIDENCE_LABELS.claim_limit, "ATTESTATION_NOT_STORAGE_PROOF");
  assert.equal(schema.properties.claim_limit.const, EVIDENCE_LABELS.claim_limit);
  assert.deepEqual([...schema.properties.kind.enum].sort(), Object.keys(EVIDENCE_LABELS.modes).sort());
});

test("the denylist rules are well formed and are exactly the list of this test", () => {
  const rules = loadRules();
  for (const phrase of rules.denylist) {
    assert.match(phrase, /^[a-z][a-z -]*[a-z]$/, phrase);
  }
  assert.equal(new Set(rules.denylist).size, rules.denylist.length, "a denied phrase repeats");
  assert.deepEqual([...rules.denylist].sort(), [...DENYLIST].sort());
  const catalogSentences = humanTexts().flatMap(([, text]) => sentences(text));
  assert.equal(new Set(rules.negation_allowlist).size, rules.negation_allowlist.length, "an allowed sentence repeats");
  for (const s of rules.negation_allowlist) {
    assert.ok(catalogSentences.includes(s), `allowed sentence not in the catalog: ${s}`);
    assert.ok(deniedIn(rules.denylist, s).length > 0, `allowed sentence holds no denied phrase: ${s}`);
    assert.match(s, NEGATION, `allowed sentence does not negate: ${s}`);
  }
});

test("no catalog string claims proof of storage, a unique replica, independence, legality, or future quality", () => {
  const rules = loadRules();
  const found = humanTexts().flatMap(([path, text]) => violations(rules, text).map((v) => `${path}: ${v}`));
  assert.deepEqual(found, []);
});

test("the denylist scanner detects denied claims and passes the allowed negations", () => {
  const rules = loadRules();
  for (const phrase of rules.denylist) {
    for (const s of [phrase, phrase.toUpperCase(), "This is " + phrase + " here."]) {
      assert.ok(violations(rules, s).length > 0, s);
    }
  }
  for (const s of [
    "A Merkle inclusion shows a unique replica.",
    "It is not proof of storage. It is proof of storage.",
    "It is not proof of storage, but it is lawful.",
    "Independently operated nodes hold verified copies.",
    "Future quality is guaranteed.",
    "This is a STORAGE_PROOF.",
    "A storageProof was recorded.",
    "The chain verifies storage.",
    "The auditor is honest.",
    "The record extends the lease.",
    "This beta is production ready.",
  ]) {
    assert.ok(violations(rules, s).length > 0, s);
  }
  for (const s of rules.negation_allowlist) {
    assert.deepEqual(violations(rules, s), []);
  }
  for (const s of ["An approved improvement.", "Unverified host clock."]) {
    assert.deepEqual(violations(rules, s), [], s);
  }
});
