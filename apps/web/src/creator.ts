/**
 * Creator publishing. Status comes from the upload service, the scheduler
 * read, and the indexer. A local flag is not publication.
 * Media bytes for an upload id are not replaced. A shorter trim is not
 * written, because this client does not rewrite the file.
 */

import { ApiError, ID, isRecord, type Aggregates } from "./model.ts";

export const PROPOSED_MAX_INPUT_BYTES = 100_000_000;
export const PROPOSED_MAX_DURATION_SECONDS = 60;
export const MAX_CAPTION_BYTES = 65_536;
export const MAX_TITLE = 120;
export const MAX_DESCRIPTION = 2_000;
export const MAX_DRAFTS = 20;

export const RECOVERY_TEXT = "Account recovery is not available. DEC-001 is unset.";
export const PUBLISHED_TEXT =
  "Published means the moderation and hosting conditions in this read are satisfied. It does not mean the chain certified this video as lawful or truthful.";
export const REVIEW_TEXT =
  "Review pending. Goal 23 moderation is not implemented, so this screen cannot complete review. This is not Published.";
export const REMOVAL_TEXT =
  "Removal was not sent. No creator removal route exists. Goal 23 owns moderation removal. This does not delete copies and it is not universal deletion.";
export const WITHDRAWN_TEXT =
  "Withdrawn. Playback is not requested. This is not universal deletion.";
export const TRIM_TEXT =
  "This client does not rewrite media bytes. A shorter range is not stored. The server probes the uploaded bytes.";
export const CAPTION_TEXT =
  "Captions are checked in this browser. The upload API does not accept a caption file, so this text is not sent to the worker.";
export const TITLE_TEXT =
  "Title and description stay in this browser. They are not written to the chain.";
export const THUMB_TEXT =
  "The worker writes one thumbnail. Choosing another frame is not available.";
export const LICENCE_TEXT =
  "The creator licence is unset. DEC-006 is open. This screen does not show a licence grant.";

const UPLOAD_ID = /^u[0-9a-f]{32}$/;
const CONTENT_ID = /^c[0-9a-f]{32}$/;
const KEY = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$/;
const STAGES = ["Uploading", "Processing", "Review pending", "Replicating", "Published"] as const;

export type LimitView = {
  label: string;
  max_input_bytes: number;
  max_duration_seconds: number;
};

export const PROPOSED_LIMITS: LimitView = {
  label: "PROPOSED",
  max_input_bytes: PROPOSED_MAX_INPUT_BYTES,
  max_duration_seconds: PROPOSED_MAX_DURATION_SECONDS,
};

export type Draft = {
  localId: string;
  ownerAccountId: string;
  idempotencyKey: string;
  title: string;
  description: string;
  captions: string;
  trimStart: string;
  trimEnd: string;
  knownDuration: string;
  fileName: string;
  declaredBytes: number;
  digest: string;
  uploadId: string;
  contentId: string;
  contentVersion: number | null;
  removalRequested: boolean;
};

export type UploadView = {
  upload_id: string;
  content_id: string;
  status: string;
  declared_bytes: number;
  received_bytes: number;
  sha256: string;
  job_status: string;
  publication: string;
  limits_label: string;
  duration_seconds: number | null;
};

export type TransferView = {
  token: string;
  expires_at: string;
  method: string;
  path: string;
};

export type IntentView = {
  upload_id: string;
  content_id: string;
  status: string;
  limits_label: string;
  limits: LimitView;
  transfer: TransferView | null;
};

export type AvailabilityView = {
  content_id: string;
  publication: string;
  publication_reason: string;
  community_approval: boolean;
  reachability: string;
  at_risk: boolean;
  renewal: string;
  degraded_providers: string[];
  simulated_labels: boolean;
  independent_operator_verification: boolean;
  storage_proof: boolean;
  active_commitments: number;
  verified_transfers: number;
};

export type ChainOperation = { status: string };

export type ComposeInput = {
  ownerAccountId: string;
  sessionAccountId: string;
  optimisticPublished: boolean;
  upload: UploadView | null;
  availability: AvailabilityView | null;
  chain: ChainOperation | null;
  indexerStale: boolean;
  withdrawn: boolean;
  activeLeaseProviders: string[];
  removalRequested: boolean;
  mediaStatus: string;
  playbackPublication: string;
};

export type CreatorState = {
  stage: string;
  published: boolean;
  notices: string[];
  chainPhase: string;
  canEdit: boolean;
  progressValue: number | null;
  progressMax: number | null;
};

export type ActionResult = {
  ok: boolean;
  code: string;
  idempotent: boolean;
  published: boolean;
  sent: boolean;
  upload: UploadView | null;
  draft: Draft | null;
};

export function emptyDraft(owner = ""): Draft {
  return {
    localId: newId("d"),
    ownerAccountId: owner,
    idempotencyKey: newId("k"),
    title: "",
    description: "",
    captions: "",
    trimStart: "0",
    trimEnd: "",
    knownDuration: "",
    fileName: "",
    declaredBytes: 0,
    digest: "",
    uploadId: "",
    contentId: "",
    contentVersion: null,
    removalRequested: false,
  };
}

export function newId(prefix: string): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  const hex = [...bytes].map((item) => item.toString(16).padStart(2, "0")).join("");
  return prefix + hex;
}

export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const copy = new ArrayBuffer(bytes.byteLength);
  new Uint8Array(copy).set(bytes);
  const digest = await crypto.subtle.digest("SHA-256", copy);
  return [...new Uint8Array(digest)].map((item) => item.toString(16).padStart(2, "0")).join("");
}

export function effectiveLimits(view: LimitView | null): LimitView {
  const raw = view ?? PROPOSED_LIMITS;
  return {
    label: "PROPOSED",
    max_input_bytes: clamp(raw.max_input_bytes, 1, PROPOSED_LIMITS.max_input_bytes),
    max_duration_seconds: clamp(raw.max_duration_seconds, 1, PROPOSED_LIMITS.max_duration_seconds),
  };
}

function clamp(value: number, low: number, high: number): number {
  if (!Number.isFinite(value)) return high;
  return Math.min(high, Math.max(low, Math.floor(value)));
}

export function validateTrim(
  start: number,
  end: number,
  knownDuration: number | null,
  maxDuration: number,
): { ok: true } | { ok: false; code: string } {
  if (!Number.isFinite(start) || !Number.isFinite(end) || !Number.isFinite(maxDuration) || maxDuration <= 0) {
    return { ok: false, code: "TRIM_RANGE" };
  }
  if (start < 0 || end <= start) return { ok: false, code: "TRIM_RANGE" };
  if (end - start > maxDuration + 1e-9) return { ok: false, code: "DURATION" };
  if (knownDuration === null) return { ok: true };
  if (!Number.isFinite(knownDuration) || knownDuration <= 0 || end > knownDuration + 1e-6) {
    return { ok: false, code: "TRIM_RANGE" };
  }
  if (end - start + 1e-6 < knownDuration) return { ok: false, code: "TRIM_NOT_APPLIED" };
  return { ok: true };
}

export function validateCaptions(text: string): { ok: true } | { ok: false; code: string } {
  if (text === "") return { ok: true };
  if (text.includes("\u0000") || text.length > MAX_CAPTION_BYTES) return { ok: false, code: "CAPTION_REJECTED" };
  if (text.startsWith("\ufeff") || !text.startsWith("WEBVTT")) return { ok: false, code: "CAPTION_REJECTED" };
  return { ok: true };
}

export function validateText(title: string, description: string): { ok: true } | { ok: false; code: string } {
  if (title.includes("\u0000") || description.includes("\u0000")) return { ok: false, code: "MALFORMED" };
  if (title.length > MAX_TITLE || description.length > MAX_DESCRIPTION) return { ok: false, code: "LIMIT" };
  return { ok: true };
}

export function assignFile(draft: Draft, size: number, name: string): { draft: Draft; code: string } {
  const safeName = name.replace(/[/\\]/g, "").slice(0, 80);
  if (draft.uploadId && size !== draft.declaredBytes) return { draft, code: "IMMUTABLE" };
  if (draft.contentVersion !== null && size !== draft.declaredBytes) return { draft, code: "IMMUTABLE" };
  if (!draft.uploadId && draft.declaredBytes > 0 && size !== draft.declaredBytes) {
    return { draft: { ...draft, declaredBytes: size, fileName: safeName, digest: "", idempotencyKey: newId("k") }, code: "" };
  }
  return { draft: { ...draft, declaredBytes: size, fileName: safeName }, code: "" };
}

export function newVersion(draft: Draft): Draft {
  return {
    ...emptyDraft(draft.ownerAccountId),
    title: draft.title,
    description: draft.description,
    captions: draft.captions,
  };
}

export function hostingTerms(limits: LimitView | null): string[] {
  const shown = limits ?? PROPOSED_LIMITS;
  return [
    `Hosting limits from the last response are labeled ${shown.label || "PROPOSED"}. They are not an approved allowance.`,
    `The local check uses at most ${effectiveLimits(shown).max_input_bytes} bytes and ${effectiveLimits(shown).max_duration_seconds} seconds. The server enforces its own caps.`,
    "Three copies are a proposed publication parameter, not a durability guarantee.",
    LICENCE_TEXT,
    "No revenue and no storage guarantee are offered.",
  ];
}

export function analyticsLines(aggregates: Aggregates | null): string[] {
  if (!aggregates) {
    return ["No creator analytics are implemented. Views, revenue, and storage guarantees are not shown."];
  }
  return [
    "These numbers are this browser's playback counters. They are not views, not revenue, and not a network census.",
    `Playback errors ${aggregates.playback_errors}. Rendered ${aggregates.rendered}. Plays ${aggregates.plays}.`,
    `Delivered bytes ${aggregates.delivered_bytes}. HTTP responses ${aggregates.http_responses}.`,
  ];
}

export function explainError(code: string): string {
  switch (code) {
    case "UNAVAILABLE":
      return "The network request did not finish. Nothing was marked published.";
    case "UNAUTHENTICATED":
      return "The upload authorization is missing or expired.";
    case "LIMIT":
      return "The file is over the proposed size limit.";
    case "QUOTA_INPUT":
    case "QUOTA_OUTPUT":
    case "NO_ALLOWANCE":
      return "The account allowance is exhausted. This is a proposed cap, not a storage guarantee.";
    case "MALFORMED":
    case "DURATION":
    case "DIMENSIONS":
      return "The content was rejected.";
    case "PUBLICATION_BLOCKED":
      return "Publication was refused. Public launch is disabled. This video is not Published.";
    case "NOT_FOUND":
      return "The upload was not found for this account.";
    case "NOT_OWNER":
      return "This account does not own the upload. The action was not sent.";
    case "ROLE":
      return "This role cannot upload.";
    case "CONFLICT":
    case "IMMUTABLE":
      return "Media bytes under this upload are not changed. A different file is a new upload.";
    case "TRIM_RANGE":
      return "That trim range is not allowed.";
    case "TRIM_NOT_APPLIED":
      return TRIM_TEXT;
    case "CAPTION_REJECTED":
      return "The captions are not a supported text file.";
    case "CONFIRMATION_REQUIRED":
      return "This action needs the confirmation word.";
    case "REMOVAL_UNAVAILABLE":
      return REMOVAL_TEXT;
    case "STATE":
      return "The upload is not in a state that accepts this action.";
    default:
      return "The request did not finish.";
  }
}

export function chainPhase(chain: ChainOperation | null): string {
  if (!chain) return "not read";
  switch (chain.status) {
    case "OPERATION_STATUS_REQUESTED":
      return "requested";
    case "OPERATION_STATUS_SUBMITTED":
      return "submitted";
    case "OPERATION_STATUS_COMMITTED":
      return "committed";
    case "OPERATION_STATUS_FAILED":
      return "failed";
    case "OPERATION_STATUS_PENDING_STALL":
      return "pending";
    default:
      return "not read";
  }
}

export function composeCreator(input: ComposeInput): CreatorState {
  const notices: string[] = [];
  const phase = chainPhase(input.chain);
  const canEdit = input.sessionAccountId !== "" && (input.ownerAccountId === "" || input.ownerAccountId === input.sessionAccountId);
  if (input.ownerAccountId && input.sessionAccountId && input.ownerAccountId !== input.sessionAccountId) {
    notices.push("This account does not own the upload.");
  }
  if (input.optimisticPublished) {
    notices.push("A local publish flag is ignored. Published is not shown from this browser alone.");
  }
  if (input.indexerStale || input.availability?.publication === "QUEUED_NOT_COMMITTED" || phase === "pending") {
    notices.push("Chain data is stalled. This operation stays pending. A local record is not a storage promise.");
  }
  if (phase === "requested") notices.push("Requested. The chain has not committed this operation.");
  if (phase === "submitted") notices.push("Submitted. A broadcast acknowledgement is not committed execution.");
  if (phase === "committed") notices.push("Committed. A committed record is not by itself publication.");
  if (phase === "failed") notices.push("The chain operation failed.");
  if (input.availability) {
    const view = input.availability;
    if (view.at_risk || view.reachability === "REACHABILITY_DEGRADED" || view.degraded_providers.length > 0) {
      notices.push("Availability degraded.");
    }
    if (view.renewal === "RENEWAL_NEEDED" || view.renewal === "EXPIRED") notices.push("Hosting renewal needed.");
    if (view.simulated_labels || view.independent_operator_verification === false) {
      notices.push("Operator labels in this read are not independent-operator verification.");
    }
    if (view.storage_proof === false) notices.push("This read is not a storage proof and not a durability guarantee.");
  } else {
    notices.push("Hosting status is not loaded. Published is not shown from a missing read.");
  }
  if (input.playbackPublication && input.playbackPublication !== "PUBLISHED") {
    notices.push("The playback document is not the publication record.");
  }
  if (input.withdrawn) {
    notices.push(WITHDRAWN_TEXT);
    if (input.activeLeaseProviders.length === 0) {
      notices.push("No active commitment is in this read. That is not proof that every host deleted the bytes.");
    }
    for (const provider of input.activeLeaseProviders) {
      if (ID.test(provider)) notices.push(`Participating host ${provider} still has an active commitment.`);
    }
  } else if (input.removalRequested) {
    notices.push(REMOVAL_TEXT);
  }
  const upload = input.upload;
  const received = upload?.received_bytes ?? 0;
  const declared = upload?.declared_bytes ?? 0;
  const progressValue = upload && declared > 0 ? received : null;
  const progressMax = upload && declared > 0 ? declared : null;
  const base = { notices, chainPhase: phase, canEdit, progressValue, progressMax, published: false };
  if (upload && (upload.status === "REJECTED" || upload.status === "EXPIRED")) {
    notices.push(upload.status === "EXPIRED" ? explainError("UNAUTHENTICATED") : explainError("MALFORMED"));
    return { ...base, stage: "Failed" };
  }
  if (upload && upload.status === "CANCELLED") {
    notices.push("The upload reservation was released. A worker that already claimed the job can still finish a package. That package is not published.");
    return { ...base, stage: "Cancelled" };
  }
  if (publishedNow(input)) {
    notices.push(PUBLISHED_TEXT);
    notices.push("Goal 23 moderation completion is not this screen. Published is shown because this read already says the approval and hosting conditions hold.");
    return { ...base, stage: "Published", published: true };
  }
  const uploading = upload !== null && (upload.status === "RESERVED" || upload.status === "RECEIVING" || upload.status === "RECEIVED");
  if (uploading) {
    notices.push(received > 0 ? `Uploading. Received ${received} of ${declared} bytes.` : "Uploading.");
    return { ...base, stage: "Uploading" };
  }
  const ready = input.mediaStatus === "READY_FOR_REVIEW" || upload?.job_status === "READY_FOR_REVIEW";
  const processing = upload !== null && (upload.status === "PROBING" || upload.status === "PROCESSING_QUEUED");
  const withheld = input.availability?.publication === "WITHHELD";
  const reason = input.availability?.publication_reason ?? "";
  const approved = input.availability?.community_approval === true;
  if (processing && !ready) {
    notices.push("Processing. Review pending is not shown from a queued job alone.");
    notices.push(REVIEW_TEXT);
    return { ...base, stage: "Processing" };
  }
  if (withheld && approved && (reason === "verified_transfer" || reason === "operator_distinct_commitments")) {
    notices.push("Replicating. Publication waits for the hosting conditions in this read.");
    return { ...base, stage: "Replicating" };
  }
  if (ready || (withheld && (!approved || reason === "community_approval"))) {
    notices.push(REVIEW_TEXT);
    return { ...base, stage: "Review pending" };
  }
  if (processing) return { ...base, stage: "Processing" };
  notices.push("Draft. Nothing has been uploaded. Published is not shown.");
  notices.push(TITLE_TEXT);
  notices.push(CAPTION_TEXT);
  notices.push(THUMB_TEXT);
  notices.push(TRIM_TEXT);
  return { ...base, stage: "Draft" };
}

function publishedNow(input: ComposeInput): boolean {
  const view = input.availability;
  if (!view || view.publication !== "PUBLISHED" || view.community_approval !== true) return false;
  if (input.indexerStale || input.withdrawn) return false;
  if (input.chain && chainPhase(input.chain) !== "committed") return false;
  if (input.optimisticPublished && view.publication !== "PUBLISHED") return false;
  return true;
}

export function stageNames(): readonly string[] {
  return STAGES;
}

export type Transport = {
  postJson: (url: string, body: unknown, token: string) => Promise<unknown>;
  putBytes: (url: string, bytes: Uint8Array, token: string) => Promise<unknown>;
  getJson: (url: string, token: string) => Promise<unknown>;
  deleteJson: (url: string, token: string) => Promise<unknown>;
};

export type SessionAuth = { accountId: string; token: string; role: string };

const DONE = new Set(["PROCESSING_QUEUED", "CANCELLED", "REJECTED", "EXPIRED", "PROBING"]);

export async function performUpload(
  draft: Draft,
  file: Uint8Array,
  limits: LimitView | null,
  session: SessionAuth,
  base: string,
  transport: Transport,
): Promise<ActionResult> {
  const stamped = draft.ownerAccountId ? draft : { ...draft, ownerAccountId: session.accountId };
  const owned = own(stamped, session);
  if (owned) return failure(owned, stamped);
  if (session.role && session.role !== "creator") return failure("ROLE", stamped);
  const text = validateText(stamped.title, stamped.description);
  if (!text.ok) return failure(text.code, stamped);
  const captions = validateCaptions(stamped.captions);
  if (!captions.ok) return failure(captions.code, stamped);
  const known = stamped.knownDuration === "" ? null : Number(stamped.knownDuration);
  const cap = effectiveLimits(limits);
  const trim = validateTrim(Number(stamped.trimStart), Number(stamped.trimEnd), known, cap.max_duration_seconds);
  if (!trim.ok) return failure(trim.code, stamped);
  if (file.byteLength < 1) return failure("MALFORMED", stamped);
  if (file.byteLength > cap.max_input_bytes) return failure("LIMIT", stamped);
  if (stamped.uploadId && stamped.declaredBytes !== file.byteLength) return failure("IMMUTABLE", stamped);
  if (!KEY.test(stamped.idempotencyKey)) return failure("MALFORMED", stamped);
  const digest = await sha256Hex(file);
  if (stamped.digest && stamped.digest !== digest) return failure("IMMUTABLE", stamped);
  let intent: IntentView;
  try {
    intent = parseIntent(await transport.postJson(join(base, "/upload-intents"), {
      idempotency_key: stamped.idempotencyKey,
      declared_bytes: file.byteLength,
      client_duration_seconds: Number(stamped.trimEnd) - Number(stamped.trimStart),
      filename: stamped.fileName || "video",
      mime: "application/octet-stream",
    }, session.token));
  } catch (error) {
    return failure(codeOf(error), stamped);
  }
  if (stamped.uploadId && intent.upload_id !== stamped.uploadId) return failure("CONFLICT", stamped);
  const idempotent = stamped.uploadId === intent.upload_id && stamped.uploadId !== "";
  const next = applyIntent(stamped, intent, file.byteLength);
  if (DONE.has(intent.status)) {
    const code = intent.status === "REJECTED" ? "MALFORMED" : intent.status === "EXPIRED" ? "UNAUTHENTICATED" : "";
    return {
      ok: code === "",
      code,
      idempotent: true,
      published: false,
      sent: false,
      upload: uploadFromIntent(intent, file.byteLength),
      draft: next,
    };
  }
  if (!intent.transfer) {
    try {
      const upload = parseUpload(await transport.getJson(join(base, `/uploads/${intent.upload_id}`), session.token));
      return { ok: true, code: "", idempotent: true, published: false, sent: false, upload, draft: applyUpload(next, upload) };
    } catch (error) {
      return failure(codeOf(error), next);
    }
  }
  if (intent.transfer.method !== "PUT" || !intent.transfer.path.startsWith("/uploads/")) {
    return failure("MALFORMED", next);
  }
  try {
    await transport.putBytes(join(base, intent.transfer.path), file, intent.transfer.token);
  } catch (error) {
    return failure(codeOf(error), next);
  }
  const stored = { ...next, digest };
  try {
    const upload = parseUpload(await transport.postJson(join(base, `/uploads/${intent.upload_id}/complete`), { digest }, session.token));
    return { ok: true, code: "", idempotent, published: false, sent: true, upload, draft: applyUpload(stored, upload) };
  } catch (error) {
    return failure(codeOf(error), stored);
  }
}

export async function performCancel(
  draft: Draft,
  confirm: string,
  session: SessionAuth,
  base: string,
  transport: Transport,
): Promise<ActionResult> {
  if (confirm !== "cancel") return failure("CONFIRMATION_REQUIRED", draft);
  const owned = own(draft, session);
  if (owned) return failure(owned, draft);
  if (!UPLOAD_ID.test(draft.uploadId)) return failure("NOT_FOUND", draft);
  try {
    const upload = parseUpload(await transport.deleteJson(join(base, `/uploads/${draft.uploadId}`), session.token));
    return { ok: true, code: "", idempotent: upload.status === "CANCELLED", published: false, sent: true, upload, draft: applyUpload(draft, upload) };
  } catch (error) {
    return failure(codeOf(error), draft);
  }
}

export async function performPublish(
  draft: Draft,
  confirm: string,
  session: SessionAuth,
  base: string,
  transport: Transport,
): Promise<ActionResult> {
  if (confirm !== "publish") return failure("CONFIRMATION_REQUIRED", draft);
  const owned = own(draft, session);
  if (owned) return failure(owned, draft);
  try {
    await transport.postJson(join(base, "/publish"), { upload_id: draft.uploadId }, session.token);
  } catch (error) {
    const code = codeOf(error);
    return { ...failure(code, draft), published: false };
  }
  return { ok: true, code: "NOT_PUBLISHED", idempotent: false, published: false, sent: true, upload: null, draft };
}

export function performRemoval(draft: Draft, confirm: string, session: SessionAuth): ActionResult {
  if (confirm !== "withdraw") return failure("CONFIRMATION_REQUIRED", draft);
  const owned = own(draft, session);
  if (owned) return failure(owned, draft);
  return {
    ok: true,
    code: "REMOVAL_UNAVAILABLE",
    idempotent: draft.removalRequested,
    published: false,
    sent: false,
    upload: null,
    draft: { ...draft, removalRequested: true },
  };
}

export function shareInflight<T>(map: Map<string, Promise<T>>, key: string, start: () => Promise<T>): Promise<T> {
  const existing = map.get(key);
  if (existing) return existing;
  const promise = start().finally(() => {
    if (map.get(key) === promise) map.delete(key);
  });
  map.set(key, promise);
  return promise;
}

function own(draft: Draft, session: SessionAuth): string {
  if (!session.token || !session.accountId) return "UNAUTHENTICATED";
  if (!draft.ownerAccountId) return "";
  if (draft.ownerAccountId !== session.accountId) return "NOT_OWNER";
  return "";
}

function failure(code: string, draft: Draft): ActionResult {
  return { ok: false, code, idempotent: false, published: false, sent: false, upload: null, draft };
}

function applyIntent(draft: Draft, intent: IntentView, size: number): Draft {
  return {
    ...draft,
    uploadId: intent.upload_id,
    contentId: intent.content_id || draft.contentId,
    declaredBytes: size,
    ownerAccountId: draft.ownerAccountId,
  };
}

function applyUpload(draft: Draft, upload: UploadView): Draft {
  return {
    ...draft,
    uploadId: upload.upload_id || draft.uploadId,
    contentId: upload.content_id || draft.contentId,
    declaredBytes: upload.declared_bytes || draft.declaredBytes,
    digest: upload.sha256 || draft.digest,
  };
}

function uploadFromIntent(intent: IntentView, size: number): UploadView {
  return {
    upload_id: intent.upload_id,
    content_id: intent.content_id,
    status: intent.status,
    declared_bytes: size,
    received_bytes: 0,
    sha256: "",
    job_status: "",
    publication: "NOT_PUBLISHED",
    limits_label: intent.limits_label,
    duration_seconds: null,
  };
}

function codeOf(error: unknown): string {
  return error instanceof ApiError ? error.code : "UNAVAILABLE";
}

function join(base: string, path: string): string {
  return base.replace(/\/$/, "") + path;
}

export function parseIntent(value: unknown): IntentView {
  if (!isRecord(value) || !UPLOAD_ID.test(stringField(value.upload_id))) throw new ApiError("DECODE", 0);
  const limits = isRecord(value.limits) ? value.limits : {};
  let transfer: TransferView | null = null;
  if (value.transfer !== undefined && value.transfer !== null) {
    if (!isRecord(value.transfer)) throw new ApiError("DECODE", 0);
    const path = stringField(value.transfer.path);
    const token = stringField(value.transfer.token);
    if (!path.startsWith("/uploads/") || path.includes("..") || !token || token.length > 200) {
      throw new ApiError("DECODE", 0);
    }
    transfer = {
      token,
      expires_at: stringField(value.transfer.expires_at),
      method: stringField(value.transfer.method),
      path,
    };
  }
  return {
    upload_id: stringField(value.upload_id),
    content_id: contentField(value.content_id),
    status: stringField(value.status),
    limits_label: stringField(value.limits_label),
    limits: {
      label: stringField(limits.label),
      max_input_bytes: numberField(limits.max_input_bytes),
      max_duration_seconds: numberField(limits.max_duration_seconds),
    },
    transfer,
  };
}

export function parseUpload(value: unknown): UploadView {
  if (!isRecord(value) || !UPLOAD_ID.test(stringField(value.upload_id))) throw new ApiError("DECODE", 0);
  const duration = value.duration_seconds;
  return {
    upload_id: stringField(value.upload_id),
    content_id: contentField(value.content_id),
    status: stringField(value.status),
    declared_bytes: numberField(value.declared_bytes),
    received_bytes: numberField(value.received_bytes),
    sha256: stringField(value.sha256),
    job_status: stringField(value.job_status),
    publication: stringField(value.publication) || "NOT_PUBLISHED",
    limits_label: stringField(value.limits_label),
    duration_seconds: typeof duration === "number" && Number.isFinite(duration) ? duration : null,
  };
}

export function parseAvailability(value: unknown): AvailabilityView {
  if (!isRecord(value) || !CONTENT_ID.test(stringField(value.content_id))) throw new ApiError("DECODE", 0);
  const degraded = Array.isArray(value.degraded_providers)
    ? value.degraded_providers.filter((item): item is string => typeof item === "string" && ID.test(item))
    : [];
  return {
    content_id: stringField(value.content_id),
    publication: stringField(value.publication),
    publication_reason: stringField(value.publication_reason),
    community_approval: value.community_approval === true,
    reachability: stringField(value.reachability),
    at_risk: value.at_risk === true,
    renewal: stringField(value.renewal),
    degraded_providers: degraded,
    simulated_labels: value.simulated_labels === true,
    independent_operator_verification: value.independent_operator_verification === true,
    storage_proof: value.storage_proof === true,
    active_commitments: numberField(value.active_commitments),
    verified_transfers: numberField(value.verified_transfers),
  };
}

export function parseChain(value: unknown): ChainOperation {
  if (!isRecord(value)) throw new ApiError("DECODE", 0);
  return { status: stringField(value.status) };
}

export function parseDrafts(value: unknown): Draft[] {
  if (!Array.isArray(value)) return [];
  const drafts: Draft[] = [];
  for (const item of value) {
    const draft = parseDraft(item);
    if (draft) drafts.push(draft);
    if (drafts.length >= MAX_DRAFTS) break;
  }
  return drafts;
}

function parseDraft(value: unknown): Draft | null {
  if (!isRecord(value)) return null;
  const localId = stringField(value.localId);
  const key = stringField(value.idempotencyKey);
  if (!/^d[0-9a-f]{32}$/.test(localId) || !KEY.test(key)) return null;
  const version = value.contentVersion;
  return {
    localId,
    ownerAccountId: stringField(value.ownerAccountId),
    idempotencyKey: key,
    title: stringField(value.title).slice(0, MAX_TITLE),
    description: stringField(value.description).slice(0, MAX_DESCRIPTION),
    captions: stringField(value.captions).slice(0, MAX_CAPTION_BYTES),
    trimStart: stringField(value.trimStart).slice(0, 16),
    trimEnd: stringField(value.trimEnd).slice(0, 16),
    knownDuration: stringField(value.knownDuration).slice(0, 16),
    fileName: stringField(value.fileName).slice(0, 80),
    declaredBytes: numberField(value.declaredBytes),
    digest: /^[0-9a-f]{64}$/.test(stringField(value.digest)) ? stringField(value.digest) : "",
    uploadId: UPLOAD_ID.test(stringField(value.uploadId)) ? stringField(value.uploadId) : "",
    contentId: CONTENT_ID.test(stringField(value.contentId)) ? stringField(value.contentId) : "",
    contentVersion: typeof version === "number" && Number.isSafeInteger(version) && version > 0 ? version : null,
    removalRequested: value.removalRequested === true,
  };
}

export function publicSnapshot(state: CreatorState, code: string): string {
  return JSON.stringify({
    stage: state.stage,
    published: state.published,
    chainPhase: state.chainPhase,
    code,
    notices: state.notices,
  });
}

function contentField(value: unknown): string {
  const text = stringField(value);
  return CONTENT_ID.test(text) ? text : "";
}

function stringField(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberField(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? Math.floor(value) : 0;
}

export function availabilityUrl(base: string, contentId: string): string {
  if (!CONTENT_ID.test(contentId)) throw new ApiError("REFUSED", 0);
  return `${base.replace(/\/$/, "")}/v1/availability?content=${contentId}`;
}
