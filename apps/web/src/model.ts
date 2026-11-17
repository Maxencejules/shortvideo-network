/**
 * Typed reads of the indexer, the delivery document, and the account service.
 * Titles, captions, and comments are plain text. A bearer is never part of a URL.
 */

export const ID = /^[a-z0-9][a-z0-9._-]{0,63}$/;
export const SHA = /^[0-9a-f]{64}$/;
export const COUNT = /^(0|[1-9][0-9]*)$/;

export const PREFETCH_SEGMENTS = 1;
export const PREFETCH_ITEMS = 1;
export const ATTEMPTS_PER_HOST = 2;
export const MAX_HOSTS = 4;
export const PACKAGE_CAP = 268_435_456;
export const PLAYLIST_LIMIT = 65_536;

export const REQUIRED_DISABLED = ["ipfs", "p2p", "turn", "webrtc"] as const;

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, status: number, message?: string) {
    super(message ?? code);
    this.name = "ApiError";
    this.code = safeCode(code);
    this.status = status;
  }
}

export function safeCode(code: string): string {
  return code.replace(/[^A-Z0-9_]/gi, "").slice(0, 40) || "UNAVAILABLE";
}

export type ContentRow = {
  ID: string;
  Version: number;
  Manifest: string;
  PackageBytes: string;
  Withdrawn: boolean;
  HistoryErased: boolean;
};

export type LeaseRow = {
  ID: string;
  ProviderID: string;
  ContentID: string;
  Active: boolean;
};

export type ReadStatus = {
  chainId: string;
  indexedHeight: number | null;
  stale: boolean;
  lightClient: boolean;
  note: string;
};

export type HostRow = {
  host_id: string;
  role: string;
  base_url: string;
  verified: boolean;
};

export type SegmentRow = {
  segment_id: string;
  sha256: string;
  byte_count: string;
  path: string;
};

export type RenditionRow = {
  rendition_id: string;
  bandwidth: number;
  width: number;
  height: number;
  codecs: string;
  segments: SegmentRow[];
};

export type CaptionRow = {
  path: string;
  sha256: string;
  byte_count: string;
};

export type PlaybackDocument = {
  schema_version: number;
  adapter: string;
  content_id: string;
  publication: string;
  removal: string;
  playback_grant: string;
  chain_status: string;
  stale: boolean;
  hosting_promise: false;
  package_verified: boolean;
  hosts: HostRow[];
  disabled: string[];
  content_version: number | null;
  manifest_sha256: string | null;
  master_path: string | null;
  fallback_rendition: string | null;
  renditions: RenditionRow[];
  captions: CaptionRow | null;
};

export type FeedRow = {
  contentId: string;
  withdrawn: boolean;
  clientSelected: boolean;
};

export type Aggregates = {
  startup_ms_sum: number;
  rebuffer_ms_sum: number;
  playback_errors: number;
  delivered_bytes: number;
  playlist_bytes: number;
  other_bytes: number;
  http_responses: number;
  rendered: number;
  plays: number;
};

export function emptyAggregates(): Aggregates {
  return {
    startup_ms_sum: 0,
    rebuffer_ms_sum: 0,
    playback_errors: 0,
    delivered_bytes: 0,
    playlist_bytes: 0,
    other_bytes: 0,
    http_responses: 0,
    rendered: 0,
    plays: 0,
  };
}

export type ClientSettings = {
  deliveryBase: string;
  indexerBase: string;
  indexerBase2: string;
  accountBase: string;
  contentIds: string[];
};

export const EMPTY_SETTINGS: ClientSettings = {
  deliveryBase: "",
  indexerBase: "",
  indexerBase2: "",
  accountBase: "",
  contentIds: [],
};

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function assertNoSecretQuery(url: string): void {
  const query = url.includes("?") ? url.slice(url.indexOf("?") + 1) : "";
  if (/(^|&)(token|csrf|invite|preference|authorization|bearer)=/i.test(query)) {
    throw new ApiError("REFUSED", 0, "The request URL is not allowed to carry that value.");
  }
}

export function safeBase(value: string): string {
  if (value === "") return "";
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new ApiError("MALFORMED", 0, "The endpoint is not a loopback URL.");
  }
  if (url.username || url.password || url.search || url.hash || url.pathname.includes("..")) {
    throw new ApiError("REFUSED", 0, "The endpoint is not a loopback URL.");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new ApiError("REFUSED", 0, "The endpoint is not a loopback URL.");
  }
  if (url.hostname !== "127.0.0.1" && url.hostname !== "localhost") {
    throw new ApiError("REFUSED", 0, "The endpoint is not a loopback URL.");
  }
  return url.origin + url.pathname.replace(/\/$/, "");
}

export function joinUrl(base: string, path: string): string {
  if (!path.startsWith("/") || path.includes("..") || path.includes("?") || path.includes("#") || path.includes("\\")) {
    throw new ApiError("REFUSED", 0, "The request path is not valid.");
  }
  const url = base.replace(/\/$/, "") + path;
  assertNoSecretQuery(url);
  return url;
}

export function mediaUrl(base: string, rel: string): string {
  const parts = rel.split("/");
  if (
    !rel ||
    rel.startsWith("/") ||
    rel.includes("\\") ||
    rel.includes("://") ||
    rel.includes("?") ||
    rel.includes("#") ||
    parts.some((part) => part === "" || part === "." || part === ".." || part === "quarantine")
  ) {
    throw new ApiError("INTEGRITY", 0, "The media path is not relative.");
  }
  return `${base.replace(/\/$/, "")}/${rel}`;
}

export function plainText(value: string): string {
  return value
    .replace(/<\/?[A-Za-z][^>\n]*>/g, "")
    .replace(/[<>]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

export type Cue = { start: number; end: number; text: string };

export function parseVtt(source: string): Cue[] {
  const lines = source.replace(/^\uFEFF/, "").split(/\r?\n/);
  const cues: Cue[] = [];
  let index = 0;
  if ((lines[0] ?? "").trim().startsWith("WEBVTT")) index = 1;
  while (index < lines.length) {
    const line = (lines[index] ?? "").trim();
    index += 1;
    const timing = line.includes("-->") ? line : (lines[index] ?? "").trim();
    if (!line.includes("-->") && timing.includes("-->")) index += 1;
    if (!timing.includes("-->")) continue;
    const [startRaw, rest] = timing.split("-->");
    const start = vttClock(startRaw ?? "");
    const end = vttClock((rest ?? "").trim().split(/\s/)[0] ?? "");
    const text: string[] = [];
    while (index < lines.length && (lines[index] ?? "").trim() !== "") {
      text.push(lines[index] ?? "");
      index += 1;
    }
    if (start !== null && end !== null) {
      cues.push({ start, end, text: plainText(text.join(" ")) });
    }
  }
  return cues;
}

function vttClock(value: string): number | null {
  const trimmed = value.trim();
  const groups = /^(\d{2,}):(\d{2}):(\d{2})\.(\d{3})$/.exec(trimmed)
    ?? /^(\d{2}):(\d{2})\.(\d{3})$/.exec(trimmed);
  if (!groups) return null;
  const parsed = groups.length === 5
    ? Number(groups[1]) * 3600 + Number(groups[2]) * 60 + Number(groups[3]) + Number(groups[4]) / 1000
    : Number(groups[1]) * 60 + Number(groups[2]) + Number(groups[3]) / 1000;
  return parsed;
}

export function activeCue(cues: Cue[], seconds: number): string {
  const found = cues.find((cue) => seconds >= cue.start && seconds < cue.end);
  return found?.text ?? "";
}

function numberField(value: unknown, name: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new ApiError("DECODE", 0, name);
  }
  return value;
}

function stringField(value: unknown): string {
  return typeof value === "string" ? value : "";
}

export function parseContentList(value: unknown): ContentRow[] {
  if (!isRecord(value) || !Array.isArray(value.content)) {
    throw new ApiError("DECODE", 0, "content");
  }
  return value.content.map((item) => {
    if (!isRecord(item) || typeof item.Withdrawn !== "boolean" || typeof item.HistoryErased !== "boolean") {
      throw new ApiError("DECODE", 0, "content row");
    }
    const id = stringField(item.ID);
    if (!ID.test(id)) throw new ApiError("DECODE", 0, "content id");
    return {
      ID: id,
      Version: numberField(item.Version, "version"),
      Manifest: stringField(item.Manifest),
      PackageBytes: stringField(item.PackageBytes),
      Withdrawn: item.Withdrawn,
      HistoryErased: item.HistoryErased,
    };
  });
}

export function parseLeases(value: unknown): LeaseRow[] {
  if (!isRecord(value) || !Array.isArray(value.leases)) {
    throw new ApiError("DECODE", 0, "leases");
  }
  return value.leases.map((item) => {
    if (!isRecord(item) || typeof item.Active !== "boolean") throw new ApiError("DECODE", 0, "lease");
    return {
      ID: stringField(item.ID),
      ProviderID: stringField(item.ProviderID),
      ContentID: stringField(item.ContentID),
      Active: item.Active,
    };
  });
}

export function parseStatus(value: unknown): ReadStatus {
  if (!isRecord(value)) throw new ApiError("DECODE", 0, "status");
  const status = isRecord(value.status) ? value.status : value;
  const trust = isRecord(status.trust) ? status.trust : {};
  const height = status.indexed_height;
  return {
    chainId: stringField(status.chain_id),
    indexedHeight: typeof height === "number" && Number.isFinite(height) ? height : null,
    stale: status.stale === true,
    lightClient: trust.light_client === true,
    note: stringField(trust.note),
  };
}

function parseSegment(value: unknown): SegmentRow {
  if (!isRecord(value)) throw new ApiError("DECODE", 0, "segment");
  const path = stringField(value.path);
  const digest = stringField(value.sha256);
  const count = stringField(value.byte_count);
  if (!SHA.test(digest) || !COUNT.test(count)) throw new ApiError("INTEGRITY", 0, "segment");
  mediaUrl("https://127.0.0.1:1", path);
  return { segment_id: stringField(value.segment_id), sha256: digest, byte_count: count, path };
}

export function parsePlayback(value: unknown): PlaybackDocument {
  if (!isRecord(value) || value.schema_version !== 1 || value.hosting_promise !== false) {
    throw new ApiError("REFUSED", 0, "playback document");
  }
  const disabled = Array.isArray(value.disabled) ? value.disabled.filter((item): item is string => typeof item === "string") : [];
  if (REQUIRED_DISABLED.some((name) => !disabled.includes(name))) {
    throw new ApiError("REFUSED", 0, "disabled transports");
  }
  const hosts: HostRow[] = [];
  if (Array.isArray(value.hosts)) {
    for (const item of value.hosts) {
      if (hosts.length >= MAX_HOSTS) break;
      if (!isRecord(item) || item.verified !== true) continue;
      const hostId = stringField(item.host_id);
      const base = stringField(item.base_url);
      if (!ID.test(hostId) || !loopbackMedia(base)) continue;
      hosts.push({ host_id: hostId, role: stringField(item.role), base_url: base.replace(/\/$/, ""), verified: true });
    }
  }
  const renditions: RenditionRow[] = [];
  if (Array.isArray(value.renditions)) {
    for (const item of value.renditions) {
      if (!isRecord(item) || !Array.isArray(item.segments)) throw new ApiError("INTEGRITY", 0, "rendition");
      renditions.push({
        rendition_id: stringField(item.rendition_id),
        bandwidth: numberField(item.bandwidth, "bandwidth"),
        width: numberField(item.width, "width"),
        height: numberField(item.height, "height"),
        codecs: stringField(item.codecs),
        segments: item.segments.map(parseSegment),
      });
    }
  }
  let captions: CaptionRow | null = null;
  if (value.captions !== null && value.captions !== undefined) {
    if (!isRecord(value.captions)) throw new ApiError("INTEGRITY", 0, "captions");
    captions = {
      path: stringField(value.captions.path),
      sha256: stringField(value.captions.sha256),
      byte_count: stringField(value.captions.byte_count),
    };
    if (!SHA.test(captions.sha256) || !COUNT.test(captions.byte_count)) {
      throw new ApiError("INTEGRITY", 0, "captions");
    }
    mediaUrl("https://127.0.0.1:1", captions.path);
  }
  const contentId = stringField(value.content_id);
  if (!ID.test(contentId)) throw new ApiError("REFUSED", 0, "content id");
  return {
    schema_version: 1,
    adapter: stringField(value.adapter),
    content_id: contentId,
    publication: stringField(value.publication),
    removal: stringField(value.removal),
    playback_grant: stringField(value.playback_grant),
    chain_status: stringField(value.chain_status),
    stale: value.stale === true,
    hosting_promise: false,
    package_verified: value.package_verified === true,
    hosts,
    disabled,
    content_version: typeof value.content_version === "number" ? value.content_version : null,
    manifest_sha256: typeof value.manifest_sha256 === "string" ? value.manifest_sha256 : null,
    master_path: value.master_path === null || value.master_path === undefined ? null : stringField(value.master_path),
    fallback_rendition: typeof value.fallback_rendition === "string" ? value.fallback_rendition : null,
    renditions,
    captions,
  };
}

export function loopbackMedia(base: string): boolean {
  let url: URL;
  try {
    url = new URL(base);
  } catch {
    return false;
  }
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash || url.port === "") {
    return false;
  }
  if (url.hostname !== "127.0.0.1" && url.hostname !== "localhost") return false;
  return !url.pathname.toLowerCase().includes("quarantine");
}

export function buildFeed(rows: ContentRow[], clientIds: string[]): FeedRow[] {
  const withdrawn = new Set(rows.filter((row) => row.Withdrawn || row.HistoryErased).map((row) => row.ID));
  const seen = new Set<string>();
  const feed: FeedRow[] = [];
  const push = (contentId: string, isWithdrawn: boolean, clientSelected: boolean) => {
    if (!ID.test(contentId) || seen.has(contentId)) return;
    seen.add(contentId);
    feed.push({ contentId, withdrawn: isWithdrawn, clientSelected });
  };
  for (const row of rows) {
    if (withdrawn.has(row.ID)) continue;
    push(row.ID, false, false);
  }
  for (const id of clientIds) {
    if (!withdrawn.has(id)) push(id, false, true);
  }
  for (const id of clientIds) {
    if (withdrawn.has(id)) push(id, true, true);
  }
  for (const row of rows) {
    if (row.Withdrawn || row.HistoryErased) push(row.ID, true, false);
  }
  return feed;
}

export function blockSentence(row: FeedRow, doc: PlaybackDocument | null): string | null {
  if (row.withdrawn || doc?.removal === "REMOVED") return "Withdrawn. Playback is not requested.";
  if (!doc) return null;
  if (doc.playback_grant === "EXPIRED") return "Playback grant expired.";
  if (doc.playback_grant !== "AUTHORIZED" || !doc.package_verified || doc.hosts.length === 0) return "Unavailable.";
  return null;
}

export function statusSentences(doc: PlaybackDocument, activeLeases: number): string[] {
  const lines: string[] = [];
  if (doc.stale || doc.chain_status === "STALE") lines.push("Chain data is stale.");
  if (activeLeases > doc.hosts.length) lines.push("Availability degraded.");
  if (doc.publication !== "PUBLISHED") lines.push("Not published. A local playback grant is not publication.");
  if (doc.adapter === "origin-test") {
    lines.push("Development fixture. This is not a live creator and not a live network statistic.");
  }
  return lines;
}

export function activeLeaseCount(leases: LeaseRow[], contentId: string): number {
  return leases.filter((lease) => lease.Active && lease.ContentID === contentId).length;
}

export function readSettings(value: unknown): ClientSettings {
  const source = isRecord(value) ? value : {};
  const ids = Array.isArray(source.contentIds) ? source.contentIds.filter((item): item is string => typeof item === "string" && ID.test(item)) : [];
  return {
    deliveryBase: safeBase(stringField(source.deliveryBase)),
    indexerBase: safeBase(stringField(source.indexerBase)),
    indexerBase2: safeBase(stringField(source.indexerBase2)),
    accountBase: safeBase(stringField(source.accountBase)),
    contentIds: ids.slice(0, 50),
  };
}

export function settingsRecord(settings: ClientSettings): string {
  return JSON.stringify({
    deliveryBase: settings.deliveryBase,
    indexerBase: settings.indexerBase,
    indexerBase2: settings.indexerBase2,
    accountBase: settings.accountBase,
    contentIds: settings.contentIds,
  });
}
