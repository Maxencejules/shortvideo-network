/**
 * Invited viewer. The session bearer stays in memory and sessionStorage.
 * It is not written into a URL, a playback report, or the indexer request.
 */

import { createElement, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";

import { EVIDENCE_LABELS } from "./evidence-labels.ts";
import { getBytes, getJson, postJson } from "./http.ts";
import {
  ApiError,
  EMPTY_SETTINGS,
  ID,
  activeLeaseCount,
  blockSentence,
  buildFeed,
  emptyAggregates,
  isRecord,
  joinUrl,
  parseContentList,
  parseLeases,
  parsePlayback,
  parseStatus,
  parseVtt,
  readSettings,
  settingsRecord,
  statusSentences,
  type ClientSettings,
  type ContentRow,
  type FeedRow,
  type LeaseRow,
  type PlaybackDocument,
} from "./model.ts";
import { PlaySession, playDocument, prefetchMasters, type PlayOutcome } from "./playback.ts";
import { VIEWER_CSS } from "./style.ts";
import {
  EMPTY_PLAYER,
  EMPTY_SESSION,
  Viewer,
  useVideoReady,
  type PageName,
  type PlayerState,
  type SessionState,
} from "./ui.ts";

const METRICS = emptyAggregates();
const SESSION = new PlaySession();
let bearer = sessionStorage.getItem("svn.viewer.bearer") ?? "";

function setBearer(token: string): void {
  if (token && (token.length > 128 || /\s/.test(token))) return;
  bearer = token;
  if (token) sessionStorage.setItem("svn.viewer.bearer", token);
  else sessionStorage.removeItem("svn.viewer.bearer");
}

function App(): ReturnType<typeof createElement> {
  const [page, setPage] = useState<PageName>("home");
  const [settings, setSettings] = useState<ClientSettings>(EMPTY_SETTINGS);
  const [rows, setRows] = useState<FeedRow[]>([]);
  const [active, setActive] = useState(0);
  const [player, setPlayer] = useState<PlayerState>(EMPTY_PLAYER);
  const [session, setSession] = useState<SessionState>(EMPTY_SESSION);
  const [readLines, setReadLines] = useState<string[]>([]);
  const [reduced, setReduced] = useState(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches || localStorage.getItem("svn.viewer.motion") === "reduce");
  const [quality, setQuality] = useState<string | null>(null);
  const [replay, setReplay] = useState(0);
  const [actionMessage, setActionMessage] = useState("");
  const [ready, setReady] = useState(false);
  const leases = useRef<LeaseRow[]>([]);
  const videoNode = useRef<HTMLVideoElement | null>(null);
  const blobUrl = useRef("");
  const renderedFor = useRef("");

  useEffect(() => {
    const style = document.createElement("style");
    style.textContent = VIEWER_CSS;
    document.head.appendChild(style);
    return () => style.remove();
  }, []);

  useEffect(() => {
    document.documentElement.dataset.motion = reduced ? "reduce" : "no-preference";
    localStorage.setItem("svn.viewer.motion", reduced ? "reduce" : "system");
  }, [reduced]);

  useEffect(() => {
    const ctrl = new AbortController();
    void loadShell(ctrl.signal, {
      setSettings,
      setRows,
      setSession,
      setReadLines,
      setReady,
      leases,
    });
    return () => ctrl.abort();
  }, []);

  useEffect(() => {
    if (!ready) return;
    const generation = SESSION.generation;
    const row = rows[active];
    let url = "";
    const stop = () => {
      if (url) URL.revokeObjectURL(url);
    };
    if (!row || row.withdrawn || !settings.deliveryBase) {
      setPlayer((current) => ({
        ...current,
        contentId: row?.contentId ?? "",
        mediaUrl: "",
        sentences: row?.withdrawn ? ["Withdrawn. Playback is not requested."] : ["Unavailable."],
        details: JSON.stringify({ phase: "blocked", contentId: row?.contentId ?? "", error: row?.withdrawn ? "withdrawn" : "unavailable" }),
      }));
      return stop;
    }
    const signal = SESSION.signal();
    setPlayer((current) => ({
      ...current,
      details: JSON.stringify({ phase: "loading", contentId: row.contentId }),
    }));
    void (async () => {
      try {
        const raw = await getJson(joinUrl(settings.deliveryBase, `/v1/playback/${row.contentId}`), signal);
        if (SESSION.generation !== generation) return;
        const doc = parsePlayback(raw);
        const blocked = blockSentence(row, doc);
        const sentences = statusSentences(doc, activeLeaseCount(leases.current, row.contentId));
        if (blocked) {
          setPlayer((current) => publish(current, doc, null, [blocked, ...sentences], "", "", true));
          return;
        }
        const outcome = await playDocument(doc, getBytes, SESSION, METRICS, quality);
        if (SESSION.generation !== generation) return;
        if (!outcome.ok || !outcome.bytes) {
          setPlayer((current) => publish(current, doc, outcome, ["Unavailable.", ...sentences], "", "", true));
          return;
        }
        url = URL.createObjectURL(new Blob([toBuffer(outcome.bytes)], { type: "video/mp4" }));
        if (blobUrl.current && blobUrl.current !== url) URL.revokeObjectURL(blobUrl.current);
        blobUrl.current = url;
        const captionText = parseVtt(outcome.captions).map((cue) => cue.text).filter(Boolean).join(" ");
        setPlayer((current) => publish(current, doc, outcome, sentences, captionText, url, current.muted));
        const next = rows.slice(active + 1).find((item) => !item.withdrawn);
        if (next && settings.deliveryBase) {
          try {
            const nextDoc = parsePlayback(await getJson(joinUrl(settings.deliveryBase, `/v1/playback/${next.contentId}`), signal));
            await prefetchMasters([nextDoc], getBytes, SESSION, METRICS);
          } catch {
            return;
          }
        }
      } catch (error) {
        if (signal.aborted || SESSION.generation !== generation) return;
        const code = error instanceof ApiError ? error.code : "UNAVAILABLE";
        setPlayer((current) => ({
          ...current,
          mediaUrl: "",
          sentences: ["Unavailable."],
          details: JSON.stringify({ phase: "blocked", contentId: row.contentId, error: code }),
        }));
      }
    })();
    return () => {
      SESSION.cancel();
      stop();
    };
  }, [ready, rows, active, quality, replay, settings.deliveryBase]);

  const onReady = (video: HTMLVideoElement) => {
    const width = video.videoWidth;
    const height = video.videoHeight;
    let nonzero = 0;
    if (width > 0 && height > 0 && width * height <= 2_000_000) {
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d");
      if (ctx) {
        ctx.drawImage(video, 0, 0);
        const pixels = ctx.getImageData(0, 0, width, height).data;
        for (let index = 0; index < pixels.length; index += 4) {
          if (pixels[index] || pixels[index + 1] || pixels[index + 2]) nonzero += 1;
        }
      }
    }
    const rendered = video.readyState >= 2 && width > 0 && nonzero > 0;
    if (!rendered && video.readyState >= 2 && video.requestVideoFrameCallback && renderedFor.current !== "frame") {
      renderedFor.current = "frame";
      video.requestVideoFrameCallback(() => onReady(video));
      return;
    }
    if (rendered && renderedFor.current !== player.details.slice(0, 80)) {
      METRICS.rendered += 1;
      renderedFor.current = player.details.slice(0, 80);
    }
    setPlayer((current) => {
      const prior = parseDetails(current.details);
      return {
        ...current,
        playing: !video.paused,
        details: JSON.stringify({
          ...prior,
          rendered: METRICS.rendered > 0 && rendered,
          videoWidth: width,
          videoHeight: height,
          readyState: video.readyState,
          nonzero,
          videoCount: document.querySelectorAll("video").length,
          deliveredBytes: METRICS.delivered_bytes,
          plays: METRICS.plays,
        }),
      };
    });
  };
  const setVideo = useVideoReady(player.mediaUrl, onReady);

  return createElement(Viewer, {
    page,
    onPage: setPage,
    rows,
    active,
    onActive: (index) => {
      setQuality(null);
      setActive(index);
    },
    player,
    onQuality: setQuality,
    onToggleMute: () => setPlayer((current) => ({ ...current, muted: !current.muted })),
    onToggleCaptions: () => setPlayer((current) => ({ ...current, captionsOn: !current.captionsOn })),
    onTogglePlay: () => {
      const node = videoNode.current;
      if (!node) return;
      const wantPlay = node.paused;
      if (wantPlay) void node.play().catch(() => undefined);
      else node.pause();
      setPlayer((current) => ({ ...current, playing: wantPlay }));
    },
    onReplay: () => setReplay((value) => value + 1),
    onSave: () => void saveCurrent(settings, rows[active], setActionMessage),
    session,
    onSignIn: () => void signIn(settings, setSession, setActionMessage),
    onEnroll: (invite) => void enroll(settings, invite, setSession, setActionMessage),
    onExport: () => void accountAction(settings, "/v1/account/export", {}, "Export completed. The file stays in this browser.", setActionMessage, true),
    onBackup: () => void accountAction(settings, "/v1/account/backup", {}, "A backup key was returned once. It is not stored. Losing it is not recoverable.", setActionMessage, true),
    onDelete: (confirm) => void accountAction(settings, "/v1/account/deletion", { confirm }, "Deletion recorded. history_erased is false. This is not universal deletion.", setActionMessage, false),
    readLines,
    settings,
    onSettings: (next) => {
      try {
        const parsed = readSettings(next);
        localStorage.setItem("svn.viewer.endpoints", settingsRecord(parsed));
        setSettings(parsed);
        setActionMessage("Endpoints saved in this browser.");
      } catch (error) {
        setActionMessage(error instanceof ApiError ? "The endpoint is not a loopback URL." : "The endpoint was refused.");
      }
    },
    reducedMotion: reduced,
    onReducedMotion: setReduced,
    onVideo: (node) => {
      videoNode.current = node;
      setVideo(node);
    },
    actionMessage,
  });
}

function publish(current: PlayerState, doc: PlaybackDocument, outcome: PlayOutcome | null, sentences: string[], captionText: string, mediaUrl: string, muted: boolean): PlayerState {
  const qualities = doc.renditions.map((item) => ({
    id: item.rendition_id,
    label: `${item.rendition_id} ${item.width}x${item.height}`,
    current: outcome?.renditionId === item.rendition_id,
  }));
  return {
    ...current,
    contentId: doc.content_id,
    sentences,
    mediaUrl,
    muted,
    captionText,
    qualities,
    details: JSON.stringify({
      phase: outcome?.ok ? "ready" : "blocked",
      contentId: doc.content_id,
      rendition: outcome?.renditionId ?? "",
      sha256: outcome?.sha256 ?? "",
      caption: captionText,
      muted,
      hostsTried: outcome?.hostsTried ?? [],
      fetched: outcome?.fetched ?? [],
      skipped: outcome?.skipped ?? [],
      attempts: outcome?.attempts ?? 0,
      error: outcome?.error ?? "",
      usedFallback: outcome?.usedFallback ?? false,
      deliveredBytes: METRICS.delivered_bytes,
      plays: METRICS.plays,
      rendered: false,
      claim: EVIDENCE_LABELS.claim_limit,
    }),
  };
}

function toBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new ArrayBuffer(bytes.byteLength);
  new Uint8Array(copy).set(bytes);
  return copy;
}

type ShellSetters = {
  setSettings: (value: ClientSettings) => void;
  setRows: (value: FeedRow[]) => void;
  setSession: (value: SessionState) => void;
  setReadLines: (value: string[]) => void;
  setReady: (value: boolean) => void;
  leases: { current: LeaseRow[] };
};

async function loadShell(signal: AbortSignal, setters: ShellSetters): Promise<void> {
  let file: unknown = {};
  try {
    const response = await fetch("/viewer-config.json", { credentials: "omit", signal, redirect: "error" });
    if (response.ok) file = await response.json();
  } catch {
    file = {};
  }
  let saved: unknown = {};
  try {
    saved = JSON.parse(localStorage.getItem("svn.viewer.endpoints") ?? "{}") as unknown;
  } catch {
    saved = {};
  }
  const merged = { ...(isRecord(file) ? file : {}), ...(isRecord(saved) ? saved : {}) };
  let settings = EMPTY_SETTINGS;
  try {
    settings = readSettings(merged);
  } catch {
    settings = EMPTY_SETTINGS;
  }
  setters.setSettings(settings);
  const lines: string[] = [];
  let content: ContentRow[] = [];
  if (settings.indexerBase) {
    try {
      const status = parseStatus(await getJson(joinUrl(settings.indexerBase, "/v1/status"), bounded(signal)));
      lines.push(readLine("Indexer", status));
      if (status.lightClient) lines.push("A light_client flag in an HTTP body is not light-client verification.");
      content = parseContentList(await getJson(joinUrl(settings.indexerBase, "/v1/content"), bounded(signal)));
      setters.leases.current = parseLeases(await getJson(joinUrl(settings.indexerBase, "/v1/leases"), bounded(signal)));
    } catch (error) {
      lines.push("The indexer could not be read.");
      if (error instanceof ApiError) lines.push(`Indexer code ${error.code}.`);
    }
  }
  if (settings.indexerBase2) {
    try {
      const status = parseStatus(await getJson(joinUrl(settings.indexerBase2, "/v1/status"), bounded(signal)));
      lines.push(readLine("Second indexer", status));
    } catch {
      lines.push("The second indexer could not be read.");
    }
  }
  lines.push("The configured read gateway is trusted for reads. Two HTTP responses are not light-client verification.");
  setters.setReadLines(lines);
  setters.setRows(buildFeed(content, settings.contentIds));
  if (settings.accountBase) {
    try {
      const who = await getJson(joinUrl(settings.accountBase, "/v1/session"), bounded(signal), bearer);
      if (isRecord(who) && typeof who.account_id === "string" && ID.test(who.account_id)) {
        setters.setSession({ signedIn: true, accountId: who.account_id, role: typeof who.role === "string" ? who.role : "", message: "" });
      } else setters.setSession(EMPTY_SESSION);
    } catch (error) {
      setBearer("");
      setters.setSession({
        ...EMPTY_SESSION,
        message: error instanceof ApiError && error.code === "UNAUTHENTICATED" ? "Session is missing or expired." : "The account service could not be read.",
      });
    }
  }
  setters.setReady(true);
}

function bounded(signal: AbortSignal): AbortSignal {
  return AbortSignal.any([signal, AbortSignal.timeout(4000)]);
}

function readLine(name: string, status: { indexedHeight: number | null; stale: boolean }): string {
  const height = status.indexedHeight === null ? "unknown" : String(status.indexedHeight);
  return `${name} height ${height}.${status.stale ? " Chain data is stale." : ""} This is an indexer read, not a network census.`;
}

function parseDetails(value: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value) as unknown;
    return isRecord(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

async function signIn(settings: ClientSettings, setSession: (value: SessionState) => void, setAction: (value: string) => void): Promise<void> {
  if (!settings.accountBase || !window.PublicKeyCredential?.parseRequestOptionsFromJSON) {
    setAction("This browser has no passkey API, or the account endpoint is unset.");
    return;
  }
  try {
    const began = await postJson(joinUrl(settings.accountBase, "/v1/login/begin"), {}, AbortSignal.timeout(8000));
    const cred = await credential(began, "get");
    const done = await postJson(joinUrl(settings.accountBase, "/v1/login/finish"), cred.body, AbortSignal.timeout(8000), "", cred.ceremony);
    remember(done, setSession);
    setAction("Signed in.");
  } catch (error) {
    setAction(error instanceof ApiError ? `Sign-in returned ${error.code}.` : "Sign-in did not finish.");
  }
}

async function enroll(settings: ClientSettings, invite: string, setSession: (value: SessionState) => void, setAction: (value: string) => void): Promise<void> {
  if (!settings.accountBase || !window.PublicKeyCredential?.parseCreationOptionsFromJSON) {
    setAction("This browser has no passkey API, or the account endpoint is unset.");
    return;
  }
  try {
    const began = await postJson(joinUrl(settings.accountBase, "/v1/enroll/begin"), { invite }, AbortSignal.timeout(8000));
    const cred = await credential(began, "create");
    const done = await postJson(joinUrl(settings.accountBase, "/v1/enroll/finish"), cred.body, AbortSignal.timeout(8000), "", cred.ceremony);
    remember(done, setSession);
    setAction("Enrolled.");
  } catch (error) {
    setAction(error instanceof ApiError ? `Enrollment returned ${error.code}.` : "Enrollment did not finish.");
  }
}

async function credential(began: unknown, kind: "get" | "create"): Promise<{ ceremony: string; body: unknown }> {
  if (!isRecord(began) || typeof began.ceremony_id !== "string" || !isRecord(began.options)) {
    throw new ApiError("DECODE", 0);
  }
  const source = isRecord(began.options.publicKey) ? began.options.publicKey : began.options;
  const credentialHost = window.PublicKeyCredential;
  if (!credentialHost) throw new ApiError("UNAVAILABLE", 0);
  const result = kind === "get"
    ? await navigator.credentials.get({
        publicKey: credentialHost.parseRequestOptionsFromJSON(source as unknown as PublicKeyCredentialRequestOptionsJSON),
      })
    : await navigator.credentials.create({
        publicKey: credentialHost.parseCreationOptionsFromJSON(source as unknown as PublicKeyCredentialCreationOptionsJSON),
      });
  if (!(result instanceof PublicKeyCredential) || typeof result.toJSON !== "function") throw new ApiError("UNAVAILABLE", 0);
  return { ceremony: began.ceremony_id, body: result.toJSON() };
}

function remember(done: unknown, setSession: (value: SessionState) => void): void {
  if (!isRecord(done) || typeof done.token !== "string" || typeof done.account_id !== "string") {
    throw new ApiError("DECODE", 0);
  }
  setBearer(done.token);
  setSession({ signedIn: true, accountId: done.account_id, role: typeof done.role === "string" ? done.role : "", message: "" });
}

async function accountAction(
  settings: ClientSettings,
  path: string,
  body: unknown,
  success: string,
  setAction: (value: string) => void,
  download: boolean,
): Promise<void> {
  if (!settings.accountBase || !bearer) {
    setAction("Session is missing or expired.");
    return;
  }
  try {
    const result = await postJson(joinUrl(settings.accountBase, path), body, AbortSignal.timeout(8000), bearer);
    if (download && isRecord(result)) {
      const blob = new Blob([JSON.stringify(result)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = "svn-account.json";
      link.click();
      URL.revokeObjectURL(url);
    }
    setAction(success);
  } catch (error) {
    if (error instanceof ApiError && error.code === "UNAUTHENTICATED") {
      setBearer("");
      setAction("Session is missing or expired.");
      return;
    }
    setAction(error instanceof ApiError && error.code === "CONFIRMATION_REQUIRED" ? "This action needs a fresh passkey confirmation." : "The account request did not finish.");
  }
}

async function saveCurrent(settings: ClientSettings, row: FeedRow | undefined, setAction: (value: string) => void): Promise<void> {
  if (!row || row.withdrawn) {
    setAction("Withdrawn. Playback is not requested.");
    return;
  }
  await accountAction(settings, "/v1/saved-videos", { content_id: row.contentId }, "Saved for this account.", setAction, false);
}

const root = document.getElementById("root");
if (root) createRoot(root).render(createElement(App));
