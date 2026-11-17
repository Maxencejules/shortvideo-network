/**
 * Bounded HLS fetch. The video element receives a blob, never a playlist URL.
 * Chromium on Linux has no HLS demuxer. One segment window is hashed here.
 * Native HLS verification of every segment is not claimed.
 */

import {
  ATTEMPTS_PER_HOST,
  ApiError,
  MAX_HOSTS,
  PACKAGE_CAP,
  PLAYLIST_LIMIT,
  PREFETCH_ITEMS,
  PREFETCH_SEGMENTS,
  SHA,
  type Aggregates,
  type PlaybackDocument,
  type SegmentRow,
  mediaUrl,
} from "./model.ts";

export type ByteResponse = { status: number; body: Uint8Array };
export type ByteGet = (url: string, signal: AbortSignal, limit: number) => Promise<ByteResponse>;

export class PlaySession {
  generation = 0;
  private controller = new AbortController();

  cancel(): void {
    this.generation += 1;
    this.controller.abort();
    this.controller = new AbortController();
  }

  signal(): AbortSignal {
    return this.controller.signal;
  }
}

export type PlayOutcome = {
  ok: boolean;
  error: string;
  attempts: number;
  usedFallback: boolean;
  renditionId: string;
  bytes: Uint8Array | null;
  captions: string;
  fetched: string[];
  skipped: string[];
  hostsTried: string[];
  startupMs: number;
  sha256: string;
};

type Variant = { path: string; bandwidth: number };

export async function playDocument(
  doc: PlaybackDocument,
  get: ByteGet,
  session: PlaySession,
  metrics: Aggregates,
  rendition: string | null,
): Promise<PlayOutcome> {
  metrics.plays += 1;
  const generation = session.generation;
  const started = Date.now();
  const failed = (error: string, attempts = 0, tried: string[] = [], fetched: string[] = []): PlayOutcome => {
    if (error !== "CANCELLED") metrics.playback_errors += 1;
    return {
      ok: false,
      error,
      attempts,
      usedFallback: false,
      renditionId: "",
      bytes: null,
      captions: "",
      fetched,
      skipped: [],
      hostsTried: tried,
      startupMs: 0,
      sha256: "",
    };
  };
  try {
    const hosts = hostsOf(doc);
    if (hosts.length === 0) return failed("NO_HOST");
    const master = await across(hosts, "master.m3u8", PLAYLIST_LIMIT, get, session, generation, metrics);
    metrics.playlist_bytes += master.body.byteLength;
    const text = new TextDecoder().decode(master.body);
    const parsed = parseMaster(text);
    checkCaptions(doc, parsed.captions);
    const order = renditionOrder(doc, parsed.variants, rendition);
    const primary = rendition ?? order[0] ?? "";
    const fetched = ["master.m3u8"];
    let attempts = master.attempts;
    const tried = [...master.tried];
    let last = "INTEGRITY";
    for (const renditionId of order) {
      let media: { body: Uint8Array; hostId: string; attempts: number; tried: string[] };
      try {
        media = await across(prefer(hosts, master.hostId), `${renditionId}.m3u8`, PLAYLIST_LIMIT, get, session, generation, metrics);
      } catch (error) {
        if (stopped(error)) {
          attempts += error.attempts;
          tried.push(...error.tried);
          last = error.code;
          continue;
        }
        throw error;
      }
      attempts += media.attempts;
      tried.push(...media.tried);
      metrics.playlist_bytes += media.body.byteLength;
      fetched.push(`${renditionId}.m3u8`);
      const segments = parseMedia(new TextDecoder().decode(media.body));
      const expected = expectedSegments(doc, renditionId);
      if (expected.map((item) => item.path).join("\n") !== segments.join("\n")) {
        last = "INTEGRITY";
        continue;
      }
      const window = expected.slice(0, PREFETCH_SEGMENTS + 1);
      const skipped = expected.slice(window.length).map((item) => item.path);
      let first: Uint8Array | null = null;
      let digest = "";
      let startupMs = 0;
      let failedWindow = false;
      for (let index = 0; index < window.length; index += 1) {
        const row = window[index];
        if (!row) continue;
        const moment = Date.now();
        let part: { body: Uint8Array; hostId: string; attempts: number; tried: string[] };
        try {
          part = await across(prefer(hosts, media.hostId), row.path, PACKAGE_CAP, get, session, generation, metrics);
        } catch (error) {
          if (stopped(error)) {
            attempts += error.attempts;
            tried.push(...error.tried);
            last = error.code;
            failedWindow = true;
            break;
          }
          throw error;
        }
        attempts += part.attempts;
        tried.push(...part.tried);
        const sum = await sha256Hex(part.body);
        if (sum !== row.sha256 || String(part.body.byteLength) !== row.byte_count) {
          last = "INTEGRITY";
          failedWindow = true;
          break;
        }
        metrics.delivered_bytes += part.body.byteLength;
        fetched.push(row.path);
        if (index === 0) {
          first = part.body;
          digest = sum;
          startupMs = Date.now() - started;
        } else {
          metrics.rebuffer_ms_sum += Date.now() - moment;
        }
      }
      if (failedWindow || !first) continue;
      let captionText = "";
      if (doc.captions) {
        try {
          const caption = await across(prefer(hosts, media.hostId), doc.captions.path, Number(doc.captions.byte_count), get, session, generation, metrics);
          attempts += caption.attempts;
          tried.push(...caption.tried);
          const sum = await sha256Hex(caption.body);
          if (sum !== doc.captions.sha256 || String(caption.body.byteLength) !== doc.captions.byte_count) {
            last = "INTEGRITY";
            continue;
          }
          metrics.other_bytes += caption.body.byteLength;
          captionText = new TextDecoder().decode(caption.body);
        } catch (error) {
          if (!stopped(error)) throw error;
          attempts += error.attempts;
          tried.push(...error.tried);
          last = error.code;
          continue;
        }
      }
      metrics.startup_ms_sum += startupMs;
      return {
        ok: true,
        error: "",
        attempts,
        usedFallback: renditionId !== primary,
        renditionId,
        bytes: first,
        captions: captionText,
        fetched,
        skipped,
        hostsTried: tried,
        startupMs,
        sha256: digest,
      };
    }
    return failed(last, attempts, tried, fetched);
  } catch (error) {
    if (stopped(error)) return failed(error.code, error.attempts, error.tried);
    if (error instanceof ApiError) return failed(error.code);
    return failed("UNAVAILABLE");
  }
}

export async function prefetchMasters(
  docs: PlaybackDocument[],
  get: ByteGet,
  session: PlaySession,
  metrics: Aggregates,
): Promise<number> {
  const chosen = docs.slice(0, PREFETCH_ITEMS);
  const skipped = docs.length - chosen.length;
  const generation = session.generation;
  await Promise.all(
    chosen.map(async (doc) => {
      try {
        const hosts = hostsOf(doc);
        if (hosts.length === 0 || generation !== session.generation) return;
        const master = await across(hosts, "master.m3u8", PLAYLIST_LIMIT, get, session, generation, metrics);
        if (generation !== session.generation) return;
        metrics.playlist_bytes += master.body.byteLength;
      } catch {
        return;
      }
    }),
  );
  return skipped;
}

class Stop extends Error {
  readonly code: string;
  readonly attempts: number;
  readonly tried: string[];

  constructor(code: string, attempts: number, tried: string[]) {
    super(code);
    this.name = "Stop";
    this.code = code;
    this.attempts = attempts;
    this.tried = tried;
  }
}

function stopped(error: unknown): error is Stop {
  return error instanceof Stop;
}

function hostsOf(doc: PlaybackDocument): { id: string; base: string }[] {
  if (doc.master_path !== null && doc.master_path !== "master.m3u8") {
    throw new ApiError("INTEGRITY", 0, "master");
  }
  return doc.hosts.slice(0, MAX_HOSTS).map((host) => ({ id: host.host_id, base: host.base_url }));
}

function prefer(hosts: { id: string; base: string }[], hostId: string): { id: string; base: string }[] {
  return [...hosts].sort((left, right) => Number(left.id !== hostId) - Number(right.id !== hostId));
}

async function across(
  hosts: { id: string; base: string }[],
  rel: string,
  limit: number,
  get: ByteGet,
  session: PlaySession,
  generation: number,
  metrics: Aggregates,
): Promise<{ body: Uint8Array; hostId: string; attempts: number; tried: string[] }> {
  mediaUrl("https://127.0.0.1:1", rel);
  let attempts = 0;
  const tried: string[] = [];
  let last = "UNAVAILABLE";
  const cap = MAX_HOSTS * ATTEMPTS_PER_HOST;
  for (const host of hosts) {
    let hostAttempts = 0;
    while (hostAttempts < ATTEMPTS_PER_HOST && attempts < cap) {
      if (generation !== session.generation || session.signal().aborted) {
        throw new Stop("CANCELLED", attempts, tried);
      }
      hostAttempts += 1;
      attempts += 1;
      tried.push(host.id);
      try {
        const response = await get(mediaUrl(host.base, rel), session.signal(), limit);
        metrics.http_responses += 1;
        if (response.status === 200) {
          if (response.body.byteLength > limit) throw new ApiError("REFUSED", 0, "limit");
          return { body: response.body, hostId: host.id, attempts, tried };
        }
        if (response.status === 409) {
          last = "INTEGRITY";
          break;
        }
        last = "HTTP";
      } catch (error) {
        if (generation !== session.generation || session.signal().aborted || (error instanceof ApiError && error.code === "CANCELLED")) {
          throw new Stop("CANCELLED", attempts, tried);
        }
        last = error instanceof ApiError ? error.code : "UNAVAILABLE";
      }
    }
  }
  throw new Stop(last, attempts, tried);
}

function parseMaster(text: string): { variants: Variant[]; captions: string | null } {
  if (!text.startsWith("#EXTM3U")) throw new ApiError("INTEGRITY", 0, "master");
  const variants: Variant[] = [];
  let captions: string | null = null;
  let pending: string | null = null;
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    if (line.startsWith("#EXT-X-MEDIA:") && line.includes("TYPE=SUBTITLES")) {
      captions = attr(line, "URI");
      mediaUrl("https://127.0.0.1:1", captions);
    } else if (line.startsWith("#EXT-X-STREAM-INF:")) {
      pending = line;
    } else if (line.startsWith("#")) {
      continue;
    } else {
      mediaUrl("https://127.0.0.1:1", line);
      if (pending === null) throw new ApiError("INTEGRITY", 0, "variant");
      variants.push({ path: line, bandwidth: Number(attr(pending, "BANDWIDTH")) });
      pending = null;
    }
    if (variants.length > 8) throw new ApiError("INTEGRITY", 0, "variants");
  }
  if (pending !== null || variants.length === 0) throw new ApiError("INTEGRITY", 0, "master");
  return { variants, captions };
}

function parseMedia(text: string): string[] {
  if (!text.startsWith("#EXTM3U")) throw new ApiError("INTEGRITY", 0, "media");
  const segments: string[] = [];
  let expect = false;
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    if (line.startsWith("#EXTINF:")) expect = true;
    else if (line.startsWith("#")) continue;
    else if (expect) {
      mediaUrl("https://127.0.0.1:1", line);
      segments.push(line);
      expect = false;
    } else throw new ApiError("INTEGRITY", 0, "media");
    if (segments.length > 8) throw new ApiError("INTEGRITY", 0, "media");
  }
  if (expect || segments.length === 0) throw new ApiError("INTEGRITY", 0, "media");
  return segments;
}

function checkCaptions(doc: PlaybackDocument, ref: string | null): void {
  if (doc.captions === null) {
    if (ref !== null) throw new ApiError("INTEGRITY", 0, "captions");
    return;
  }
  if (doc.captions.path !== ref) throw new ApiError("INTEGRITY", 0, "captions");
}

function renditionOrder(doc: PlaybackDocument, variants: Variant[], requested: string | null): string[] {
  const names = variants.map((item) => (item.path.endsWith(".m3u8") ? item.path.slice(0, -5) : ""));
  if (names.some((name) => name === "")) throw new ApiError("INTEGRITY", 0, "rendition");
  const fallback = doc.fallback_rendition;
  if (fallback === null || !names.includes(fallback)) throw new ApiError("INTEGRITY", 0, "fallback");
  if (requested !== null && !names.includes(requested)) return [fallback];
  const first = requested ?? names[0] ?? fallback;
  return first === fallback ? [first] : [first, fallback];
}

function expectedSegments(doc: PlaybackDocument, renditionId: string): SegmentRow[] {
  const found = doc.renditions.find((item) => item.rendition_id === renditionId);
  if (!found || found.segments.length === 0) throw new ApiError("INTEGRITY", 0, "segments");
  return found.segments;
}

function attr(line: string, name: string): string {
  const quoted = new RegExp(`${name}="([^"]*)"`).exec(line);
  if (quoted?.[1]) return quoted[1];
  const plain = new RegExp(`${name}=([^,]*)`).exec(line);
  if (!plain?.[1]) throw new ApiError("INTEGRITY", 0, name);
  return plain[1];
}

export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  const digest = await crypto.subtle.digest("SHA-256", copy);
  const hex = [...new Uint8Array(digest)].map((item) => item.toString(16).padStart(2, "0")).join("");
  if (!SHA.test(hex)) throw new ApiError("INTEGRITY", 0, "sha256");
  return hex;
}
