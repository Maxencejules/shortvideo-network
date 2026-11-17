/**
 * Discovery reads. Content ids stay stable. Comments are plain text.
 * This client does not speak ActivityPub or AT Protocol.
 */

import { deleteJson, getJson, postJson } from "./http.ts";
import { ApiError, ID, isRecord, joinUrl, plainText } from "./model.ts";

export type SocialCard = { contentId: string; creatorId: string; title: string };
export type SocialNote = { id: string; text: string };
export type SocialHit = { id: string; kind: string; text: string };

const KEYS = new Map<string, string>();

export function commentAllowed(text: string): boolean {
  const trimmed = text.trim();
  return trimmed.length > 0 && trimmed.length <= 400 && !trimmed.includes("<") && !trimmed.includes(">");
}

export function commentKey(contentId: string, text: string): string {
  const mapKey = `${contentId}\n${text}`;
  const existing = KEYS.get(mapKey);
  if (existing) return existing;
  const created = crypto.randomUUID();
  KEYS.set(mapKey, created);
  return created;
}

export function parseFeed(value: unknown): { factors: string[]; cards: SocialCard[] } {
  if (!isRecord(value) || value.interface_version !== 1 || value.chain_events !== false) throw new ApiError("DECODE", 0);
  if (value.kind !== "following" && value.kind !== "recommended") throw new ApiError("DECODE", 0);
  const cards: SocialCard[] = [];
  for (const row of Array.isArray(value.items) ? value.items : []) {
    if (!isRecord(row) || typeof row.content_id !== "string" || !ID.test(row.content_id)) continue;
    if (typeof row.creator_id !== "string" || !ID.test(row.creator_id)) continue;
    cards.push({
      contentId: row.content_id,
      creatorId: row.creator_id,
      title: typeof row.title === "string" ? plainText(row.title) : "",
    });
  }
  const factors = Array.isArray(value.scoring_factors)
    ? value.scoring_factors.filter((item): item is string => typeof item === "string")
    : [];
  return { factors, cards };
}

export function parseNotes(value: unknown, listKey: "notes" | "comments", idKey: string): SocialNote[] {
  if (!isRecord(value) || value.interface_version !== 1) throw new ApiError("DECODE", 0);
  const notes: SocialNote[] = [];
  for (const row of Array.isArray(value[listKey]) ? value[listKey] : []) {
    if (!isRecord(row) || typeof row[idKey] !== "string" || typeof row.text !== "string") continue;
    notes.push({ id: row[idKey], text: plainText(row.text) });
  }
  return notes;
}

export function parseHits(value: unknown): SocialHit[] {
  if (!isRecord(value) || value.interface_version !== 1 || value.chain_events !== false) throw new ApiError("DECODE", 0);
  const hits: SocialHit[] = [];
  for (const row of Array.isArray(value.hits) ? value.hits : []) {
    if (!isRecord(row) || typeof row.kind !== "string" || typeof row.text !== "string") continue;
    const id = typeof row.content_id === "string" ? row.content_id : typeof row.account_id === "string" ? row.account_id : "";
    if (!ID.test(id)) continue;
    hits.push({ id, kind: row.kind, text: plainText(row.text) });
  }
  return hits;
}

export async function fetchFeed(base: string, token: string, kind: "following" | "recommended", signal: AbortSignal) {
  return parseFeed(await getJson(joinUrl(base, `/v1/feeds/${kind}`), signal, token));
}

export async function fetchActivity(base: string, token: string, signal: AbortSignal) {
  return parseNotes(await getJson(joinUrl(base, "/v1/activity"), signal, token), "notes", "note_id");
}

export async function fetchComments(base: string, token: string, contentId: string, signal: AbortSignal) {
  return parseNotes(await getJson(joinUrl(base, `/v1/comments/${contentId}`), signal, token), "comments", "comment_id");
}

export async function sendFollow(base: string, token: string, accountId: string, signal: AbortSignal) {
  await postJson(joinUrl(base, "/v1/follows"), { account_id: accountId }, signal, token);
}

export async function sendLike(base: string, token: string, contentId: string, signal: AbortSignal) {
  await postJson(joinUrl(base, "/v1/likes"), { content_id: contentId }, signal, token);
}

export async function sendSave(base: string, token: string, contentId: string, signal: AbortSignal) {
  await postJson(joinUrl(base, "/v1/saves"), { content_id: contentId }, signal, token);
}

export async function sendNotInterested(base: string, token: string, contentId: string, signal: AbortSignal) {
  await postJson(joinUrl(base, "/v1/not-interested"), { content_id: contentId }, signal, token);
}

export async function sendBlock(base: string, token: string, accountId: string, signal: AbortSignal) {
  await postJson(joinUrl(base, "/v1/blocks"), { account_id: accountId }, signal, token);
}

export async function sendReport(base: string, token: string, contentId: string, signal: AbortSignal) {
  await postJson(joinUrl(base, "/v1/reports"), {
    target_type: "content",
    target_id: contentId,
    category: "other",
    narrative: "viewer report",
  }, signal, token);
}

export async function sendComment(base: string, token: string, contentId: string, text: string, signal: AbortSignal) {
  await postJson(joinUrl(base, "/v1/comments"), { content_id: contentId, text }, signal, token, "", commentKey(contentId, text));
}

export async function sendSearch(base: string, token: string, kind: string, q: string, signal: AbortSignal) {
  return parseHits(await postJson(joinUrl(base, "/v1/search"), { kind, q }, signal, token));
}

export async function deleteComment(base: string, token: string, commentId: string, signal: AbortSignal) {
  await deleteJson(joinUrl(base, `/v1/comments/${commentId}`), signal, token);
}
