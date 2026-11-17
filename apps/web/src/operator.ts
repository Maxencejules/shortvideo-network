/**
 * Operator dashboard view model. Readings keep the class the service sent.
 * A stale value stays marked stale. Selecting storage does not select validator.
 */

export interface Reading {
  name: string;
  class: string;
  value: unknown;
  source: string;
  note?: string;
}

export const DUTIES = ["storage_delivery", "processing", "account_discovery", "validator"] as const;

export function readingText(row: Reading): string {
  const mark = row.class === "stale" ? "STALE" : row.class;
  const value = row.value === undefined || row.value === null ? "" : ` ${shown(row.value)}`;
  const note = row.note ? ` ${row.note}` : "";
  return `${row.name} ${mark}${value}${note}`;
}

export function toggleDuty(selected: readonly string[], duty: string): string[] {
  if (!DUTIES.includes(duty as (typeof DUTIES)[number])) return [...selected];
  if (selected.includes(duty)) return selected.filter((item) => item !== duty);
  return [...selected, duty];
}

export function storageForcesValidator(_selected: readonly string[]): boolean {
  return false;
}

export function visibleActions(role: string): string[] {
  if (role === "provider") return ["limits", "stop-offers", "drain", "handoff", "renewal", "disable"];
  if (role === "moderator") return ["queue"];
  if (role === "council") return ["governance"];
  return [];
}

function shown(value: unknown): string {
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}
