import assert from "node:assert/strict";
import test from "node:test";

import {
  assignFile,
  availabilityUrl,
  composeCreator,
  emptyDraft,
  explainError,
  newVersion,
  parseAvailability,
  parseDrafts,
  parseIntent,
  parseUpload,
  performCancel,
  performPublish,
  performRemoval,
  performUpload,
  sha256Hex,
  shareInflight,
  validateCaptions,
  validateTrim,
  type AvailabilityView,
  type ComposeInput,
  type Draft,
  type IntentView,
  type SessionAuth,
  type Transport,
  type UploadView,
} from "../src/creator.ts";
import { ApiError } from "../src/model.ts";

const SESSION: SessionAuth = { accountId: "acct", token: "session-token", role: "creator" };
const CONTENT = "c".padEnd(33, "b");
const UPLOAD = "u".padEnd(33, "1");

test("illegal trim ranges are refused before a request", async () => {
  const transport = fake();
  const draft = { ...emptyDraft("acct"), trimStart: "-1", trimEnd: "2", knownDuration: "" };
  const result = await performUpload(draft, bytes(8), null, SESSION, "http://127.0.0.1:9", transport);
  assert.equal(result.code, "TRIM_RANGE");
  assert.equal(transport.posts.length, 0);
  assert.equal(validateTrim(0, 0, null, 60).ok, false);
  assert.equal(validateTrim(1, 70, null, 60).ok, false);
  assert.equal(validateTrim(0, 4, 10, 60).ok, false);
  const full = validateTrim(0, 10, 10, 60);
  assert.equal(full.ok, true);
});

test("a shorter trim does not rewrite bytes", async () => {
  const transport = fake();
  const draft = { ...emptyDraft("acct"), trimStart: "0", trimEnd: "2", knownDuration: "10" };
  const result = await performUpload(draft, bytes(8), null, SESSION, "http://127.0.0.1:9", transport);
  assert.equal(result.code, "TRIM_NOT_APPLIED");
  assert.equal(result.sent, false);
  assert.match(explainError(result.code), /does not rewrite media bytes/);
});

test("an upload uses one reservation and a repeat is the same upload", async () => {
  const transport = fake();
  const draft = readyDraft();
  const first = await performUpload(draft, bytes(8), null, SESSION, "http://127.0.0.1:9", transport);
  assert.equal(first.ok, true);
  assert.equal(first.published, false);
  assert.equal(first.upload?.status, "PROCESSING_QUEUED");
  assert.equal(first.upload?.publication, "NOT_PUBLISHED");
  assert.equal(transport.posts.filter((item) => item.url.endsWith("/upload-intents")).length, 1);
  const second = await performUpload(first.draft ?? draft, bytes(8), null, SESSION, "http://127.0.0.1:9", transport);
  assert.equal(second.idempotent, true);
  assert.equal(second.upload?.upload_id, first.upload?.upload_id);
  assert.equal(transport.reservations, 1);
  assert.equal(transport.puts.length, 1);
});

test("a failed transfer does not complete and a timeout can be retried", async () => {
  const transport = fake();
  transport.failPut = "UNAVAILABLE";
  const draft = readyDraft();
  const failed = await performUpload(draft, bytes(8), null, SESSION, "http://127.0.0.1:9", transport);
  assert.equal(failed.code, "UNAVAILABLE");
  assert.equal(transport.completions, 0);
  assert.equal(failed.draft?.uploadId, UPLOAD);
  transport.failPut = "";
  const retried = await performUpload(failed.draft ?? draft, bytes(8), null, SESSION, "http://127.0.0.1:9", transport);
  assert.equal(retried.ok, true);
  assert.equal(retried.upload?.upload_id, UPLOAD);
  assert.equal(transport.reservations, 1);
});

test("expired authorization, size, quota, and rejected content stay typed", async () => {
  const expired = fake();
  expired.failPut = "UNAUTHENTICATED";
  const expiredResult = await performUpload(readyDraft(), bytes(8), null, SESSION, "http://127.0.0.1:9", expired);
  assert.equal(expiredResult.code, "UNAUTHENTICATED");
  assert.match(explainError(expiredResult.code), /expired/);
  const huge = await performUpload(readyDraft(), bytes(8), { label: "PROPOSED", max_input_bytes: 4, max_duration_seconds: 60 }, SESSION, "http://127.0.0.1:9", fake());
  assert.equal(huge.code, "LIMIT");
  const quota = fake();
  quota.failIntent = "QUOTA_INPUT";
  const quotaResult = await performUpload(readyDraft(), bytes(8), null, SESSION, "http://127.0.0.1:9", quota);
  assert.equal(quotaResult.code, "QUOTA_INPUT");
  assert.equal(quota.reservations, 0);
  const rejected = fake();
  rejected.failComplete = "DURATION";
  const rejectedResult = await performUpload(readyDraft(), bytes(8), null, SESSION, "http://127.0.0.1:9", rejected);
  assert.equal(rejectedResult.code, "DURATION");
  assert.match(explainError(rejectedResult.code), /rejected/);
});

test("another account cannot cancel, publish, or remove", async () => {
  const transport = fake();
  const draft = { ...readyDraft(), uploadId: UPLOAD, ownerAccountId: "acct" };
  const other: SessionAuth = { accountId: "other", token: "other-token", role: "creator" };
  const cancelled = await performCancel(draft, "cancel", other, "http://127.0.0.1:9", transport);
  assert.equal(cancelled.code, "NOT_OWNER");
  assert.equal(transport.deletes, 0);
  const published = await performPublish(draft, "publish", other, "http://127.0.0.1:9", transport);
  assert.equal(published.published, false);
  assert.equal(published.sent, false);
  const removed = performRemoval(draft, "withdraw", other);
  assert.equal(removed.code, "NOT_OWNER");
  assert.equal(removed.sent, false);
});

test("cancel during processing releases once and requires confirmation", async () => {
  const transport = fake();
  const draft = { ...readyDraft(), uploadId: UPLOAD, ownerAccountId: "acct" };
  const missing = await performCancel(draft, "no", SESSION, "http://127.0.0.1:9", transport);
  assert.equal(missing.code, "CONFIRMATION_REQUIRED");
  assert.equal(transport.deletes, 0);
  const done = await performCancel(draft, "cancel", SESSION, "http://127.0.0.1:9", transport);
  assert.equal(done.upload?.status, "CANCELLED");
  const again = await performCancel(done.draft ?? draft, "cancel", SESSION, "http://127.0.0.1:9", transport);
  assert.equal(again.idempotent, true);
  assert.equal(transport.deletes, 2);
  assert.equal(transport.reservations, 0);
});

test("publication intent cannot mark the video published", async () => {
  const transport = fake();
  const draft = { ...readyDraft(), uploadId: UPLOAD, ownerAccountId: "acct" };
  const result = await performPublish(draft, "publish", SESSION, "http://127.0.0.1:9", transport);
  assert.equal(result.code, "PUBLICATION_BLOCKED");
  assert.equal(result.published, false);
  const state = composeCreator(input({ optimisticPublished: true }));
  assert.equal(state.published, false);
  assert.equal(state.stage, "Draft");
  assert.match(state.notices.join(" "), /local publish flag is ignored/);
});

test("removal is not sent and does not claim universal deletion", () => {
  const draft = { ...readyDraft(), uploadId: UPLOAD, ownerAccountId: "acct", contentId: CONTENT };
  const missing = performRemoval(draft, "remove", SESSION);
  assert.equal(missing.code, "CONFIRMATION_REQUIRED");
  const result = performRemoval(draft, "withdraw", SESSION);
  assert.equal(result.sent, false);
  assert.equal(result.draft?.removalRequested, true);
  const again = performRemoval(result.draft ?? draft, "withdraw", SESSION);
  assert.equal(again.idempotent, true);
  const withdrawn = composeCreator(input({
    withdrawn: true,
    activeLeaseProviders: ["p1"],
    removalRequested: true,
    upload: upload("PROCESSING_QUEUED"),
  }));
  const text = withdrawn.notices.join(" ");
  assert.match(text, /not universal deletion/);
  assert.match(text, /Participating host p1 still has an active commitment/);
  assert.equal(withdrawn.stage === "Published", false);
});

test("lifecycle states stay distinct and Published waits for the read", () => {
  assert.equal(composeCreator(input({ upload: upload("RECEIVING", 4) })).stage, "Uploading");
  assert.equal(composeCreator(input({ upload: upload("PROCESSING_QUEUED") })).stage, "Processing");
  const review = composeCreator(input({
    upload: upload("PROCESSING_QUEUED"),
    mediaStatus: "READY_FOR_REVIEW",
    availability: availability("WITHHELD", false, "community_approval"),
  }));
  assert.equal(review.stage, "Review pending");
  assert.match(review.notices.join(" "), /Goal 23/);
  assert.equal(review.published, false);
  const replicating = composeCreator(input({
    mediaStatus: "READY_FOR_REVIEW",
    availability: availability("WITHHELD", true, "verified_transfer"),
  }));
  assert.equal(replicating.stage, "Replicating");
  const stalled = composeCreator(input({
    availability: availability("QUEUED_NOT_COMMITTED", true, ""),
    indexerStale: true,
    chain: { status: "OPERATION_STATUS_PENDING_STALL" },
  }));
  assert.equal(stalled.published, false);
  assert.equal(stalled.chainPhase, "pending");
  assert.match(stalled.notices.join(" "), /stalled/);
  const degraded = composeCreator(input({
    availability: { ...availability("PUBLISHED", true, ""), at_risk: true, renewal: "RENEWAL_NEEDED" },
    chain: { status: "OPERATION_STATUS_COMMITTED" },
  }));
  assert.equal(degraded.stage, "Published");
  assert.match(degraded.notices.join(" "), /Availability degraded/);
  assert.match(degraded.notices.join(" "), /Hosting renewal needed/);
  const published = composeCreator(input({
    availability: availability("PUBLISHED", true, ""),
    chain: { status: "OPERATION_STATUS_COMMITTED" },
  }));
  assert.equal(published.stage, "Published");
  assert.match(published.notices.join(" "), /does not mean the chain certified/);
  const submitted = composeCreator(input({
    availability: availability("PUBLISHED", true, ""),
    chain: { status: "OPERATION_STATUS_SUBMITTED" },
  }));
  assert.equal(submitted.published, false);
  assert.match(submitted.notices.join(" "), /acknowledgement is not committed/);
});

test("media bytes under one upload are not replaced", async () => {
  const draft = { ...readyDraft(), uploadId: UPLOAD, declaredBytes: 8, digest: await sha256Hex(bytes(8)) };
  const changed = assignFile(draft, 9, "other.mp4");
  assert.equal(changed.code, "IMMUTABLE");
  const same = assignFile(draft, 8, "clip.mp4");
  assert.equal(same.code, "");
  const created = newVersion(draft);
  assert.equal(created.uploadId, "");
  assert.notEqual(created.idempotencyKey, draft.idempotencyKey);
  assert.equal(created.title, draft.title);
  const transport = fake();
  const result = await performUpload({ ...draft, digest: "a".repeat(64) }, bytes(8), null, SESSION, "http://127.0.0.1:9", transport);
  assert.equal(result.code, "IMMUTABLE");
  assert.equal(transport.posts.length, 0);
});

test("captions are checked and not sent as a command", () => {
  assert.equal(validateCaptions("").ok, true);
  assert.equal(validateCaptions("WEBVTT\n\n00:00.000 --> 00:01.000\nhello").ok, true);
  assert.equal(validateCaptions("$(rm)").ok, false);
  assert.equal(validateCaptions("WEBVTT\u0000").ok, false);
});

test("parsers accept the service fields and drop a bad draft", () => {
  const intent = parseIntent(intentBody());
  assert.equal(intent.upload_id, UPLOAD);
  assert.equal(intent.transfer?.path, `/uploads/${UPLOAD}/content`);
  const upload = parseUpload({ ...intentBody(), publication: "NOT_PUBLISHED", received_bytes: 8, job_status: "QUEUED" });
  assert.equal(upload.publication, "NOT_PUBLISHED");
  const view = parseAvailability({
    content_id: CONTENT,
    publication: "WITHHELD",
    community_approval: false,
    at_risk: false,
    renewal: "NONE",
    degraded_providers: ["ok", "bad id"],
    simulated_labels: true,
    independent_operator_verification: false,
    storage_proof: false,
  });
  assert.deepEqual(view.degraded_providers, ["ok"]);
  assert.throws(() => parseUpload({ upload_id: "nope" }), ApiError);
  const drafts = parseDrafts([{ localId: "d" + "a".repeat(32), idempotencyKey: "k" + "b".repeat(20), title: "<b>" }, { localId: "bad" }]);
  assert.equal(drafts.length, 1);
  assert.equal(drafts[0]?.title, "<b>");
  assert.equal(availabilityUrl("http://127.0.0.1:9/sched", CONTENT), `http://127.0.0.1:9/sched/v1/availability?content=${CONTENT}`);
});

test("repeated clicks share one request", async () => {
  let calls = 0;
  const map = new Map<string, Promise<number>>();
  const first = shareInflight(map, "one", async () => {
    calls += 1;
    await new Promise((resolve) => setTimeout(resolve, 10));
    return calls;
  });
  const second = shareInflight(map, "one", async () => {
    calls += 1;
    return calls;
  });
  assert.equal(await first, 1);
  assert.equal(await second, 1);
  assert.equal(calls, 1);
});

function readyDraft(): Draft {
  return { ...emptyDraft("acct"), trimStart: "0", trimEnd: "1", fileName: "clip.mp4", idempotencyKey: "clip-1" };
}

function bytes(size: number): Uint8Array {
  return new Uint8Array(size).fill(7);
}

function input(extra: Partial<ComposeInput>): ComposeInput {
  return {
    ownerAccountId: "acct",
    sessionAccountId: "acct",
    optimisticPublished: false,
    upload: null,
    availability: null,
    chain: null,
    indexerStale: false,
    withdrawn: false,
    activeLeaseProviders: [],
    removalRequested: false,
    mediaStatus: "",
    playbackPublication: "",
    ...extra,
  };
}

function upload(status: string, received = 0): UploadView {
  return {
    upload_id: UPLOAD,
    content_id: CONTENT,
    status,
    declared_bytes: 8,
    received_bytes: received,
    sha256: "",
    job_status: status === "PROCESSING_QUEUED" ? "QUEUED" : "",
    publication: "NOT_PUBLISHED",
    limits_label: "PROPOSED",
    duration_seconds: null,
  };
}

function availability(publication: string, approved: boolean, reason: string): AvailabilityView {
  return {
    content_id: CONTENT,
    publication,
    publication_reason: reason,
    community_approval: approved,
    reachability: "REACHABLE",
    at_risk: false,
    renewal: "NONE",
    degraded_providers: [],
    simulated_labels: true,
    independent_operator_verification: false,
    storage_proof: false,
    active_commitments: approved ? 3 : 0,
    verified_transfers: approved ? 3 : 0,
  };
}

function intentBody(): IntentView {
  return {
    upload_id: UPLOAD,
    content_id: CONTENT,
    status: "RESERVED",
    limits_label: "PROPOSED",
    limits: { label: "PROPOSED", max_input_bytes: 100_000_000, max_duration_seconds: 60 },
    transfer: { token: "transfer-token", expires_at: "2026-11-17T00:00:00Z", method: "PUT", path: `/uploads/${UPLOAD}/content` },
  };
}

function fake(): FakeTransport {
  return new FakeTransport();
}

class FakeTransport implements Transport {
  posts: { url: string; body: unknown }[] = [];
  puts: string[] = [];
  deletes = 0;
  completions = 0;
  reservations = 0;
  failIntent = "";
  failPut = "";
  failComplete = "";
  status = "RESERVED";

  async postJson(url: string, body: unknown): Promise<unknown> {
    this.posts.push({ url, body });
    if (url.endsWith("/publish")) throw new ApiError("PUBLICATION_BLOCKED", 403);
    if (url.endsWith("/complete")) {
      if (this.failComplete) throw new ApiError(this.failComplete, 422);
      this.completions += 1;
      this.status = "PROCESSING_QUEUED";
      return upload("PROCESSING_QUEUED");
    }
    if (this.failIntent) throw new ApiError(this.failIntent, 403);
    if (this.reservations === 0) this.reservations = 1;
    const current = this.status === "PROCESSING_QUEUED" ? "PROCESSING_QUEUED" : "RESERVED";
    return { ...intentBody(), status: current, transfer: current === "RESERVED" ? intentBody().transfer : null };
  }

  async putBytes(url: string): Promise<unknown> {
    this.puts.push(url);
    if (this.failPut) throw new ApiError(this.failPut, this.failPut === "UNAUTHENTICATED" ? 401 : 0);
    this.status = "RECEIVED";
    return { status: "RECEIVED" };
  }

  async getJson(): Promise<unknown> {
    return upload(this.status);
  }

  async deleteJson(): Promise<unknown> {
    this.deletes += 1;
    this.reservations = 0;
    return upload("CANCELLED");
  }
}
