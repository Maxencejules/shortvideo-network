/**
 * Presentational viewer. User text is a React child, never HTML.
 */

import { createElement, useEffect, useRef, type KeyboardEvent, type ReactElement, type ReactNode } from "react";

import { EVIDENCE_LABELS } from "./evidence-labels.ts";
import type { ClientSettings, FeedRow } from "./model.ts";

export type PageName = "home" | "discover" | "activity" | "profile";

export type QualityChoice = { id: string; label: string; current: boolean };

export type PlayerState = {
  contentId: string;
  sentences: string[];
  mediaUrl: string;
  muted: boolean;
  captionsOn: boolean;
  captionText: string;
  qualities: QualityChoice[];
  details: string;
  playing: boolean;
};

export type SessionState = {
  signedIn: boolean;
  accountId: string;
  role: string;
  message: string;
};

export const EMPTY_PLAYER: PlayerState = {
  contentId: "",
  sentences: [],
  mediaUrl: "",
  muted: true,
  captionsOn: true,
  captionText: "",
  qualities: [],
  details: "",
  playing: false,
};

export const EMPTY_SESSION: SessionState = {
  signedIn: false,
  accountId: "",
  role: "",
  message: "Session is missing or expired.",
};

export type ViewerProps = {
  page: PageName;
  onPage: (page: PageName) => void;
  rows: FeedRow[];
  active: number;
  onActive: (index: number) => void;
  player: PlayerState;
  onQuality: (id: string) => void;
  onToggleMute: () => void;
  onToggleCaptions: () => void;
  onTogglePlay: () => void;
  onReplay: () => void;
  onSave: () => void;
  session: SessionState;
  onSignIn: () => void;
  onEnroll: (invite: string) => void;
  onExport: () => void;
  onBackup: () => void;
  onDelete: (confirm: string) => void;
  readLines: string[];
  settings: ClientSettings;
  onSettings: (next: ClientSettings) => void;
  reducedMotion: boolean;
  onReducedMotion: (value: boolean) => void;
  onVideo: (node: HTMLVideoElement | null) => void;
  actionMessage: string;
};

const PAGES: PageName[] = ["home", "discover", "activity", "profile"];

export function Viewer(props: ViewerProps): ReactElement {
  return createElement(
    "div",
    { className: props.reducedMotion ? "is-reduced" : "" },
    createElement("p", { className: "banner" }, "Invited development build. Public launch is disabled."),
    createElement("p", { className: "banner" }, "This build does not ask for a wallet, a coin, or peer upload."),
    createElement(
      "nav",
      { className: "nav", "aria-label": "Sections" },
      ...PAGES.map((page) =>
        createElement(
          "button",
          {
            type: "button",
            key: page,
            "aria-current": props.page === page ? "page" : undefined,
            onClick: () => props.onPage(page),
          },
          label(page),
        ),
      ),
    ),
    createElement(
      "main",
      { className: "layout" },
      props.page === "home" ? home(props) : null,
      props.page === "discover" ? discover(props) : null,
      props.page === "activity" ? activity() : null,
      props.page === "profile" ? profile(props) : null,
    ),
  );
}

function home(props: ViewerProps): ReactElement {
  const row = props.rows[props.active];
  return createElement(
    "div",
    { className: "stage" },
    createElement(
      "div",
      {
        id: "feed",
        className: props.reducedMotion ? "feed" : "feed smooth",
        tabIndex: 0,
        role: "region",
        "aria-label": "Videos",
        onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => onFeedKey(event, props),
      },
      props.rows.length === 0
        ? createElement("p", null, "No playable record is loaded.")
        : props.rows.map((item, index) => slide(props, item, index)),
    ),
    row ? player(props, row) : null,
  );
}

function slide(props: ViewerProps, item: FeedRow, index: number): ReactElement {
  const current = index === props.active;
  return createElement(
    "section",
    { key: item.contentId, className: "slide", "aria-label": `Content ${item.contentId}` },
    createElement("p", null, `Content ${item.contentId}`),
    item.withdrawn ? createElement("p", null, "Withdrawn. Playback is not requested.") : null,
    item.clientSelected ? createElement("p", null, "Selected in this client. This is not a live catalogue.") : null,
    current
      ? null
      : createElement(
          "button",
          { type: "button", onClick: () => props.onActive(index) },
          `Show ${item.contentId}`,
        ),
  );
}

function player(props: ViewerProps, row: FeedRow): ReactElement {
  const video = row.withdrawn || !props.player.mediaUrl
    ? null
    : createElement("video", {
        ref: props.onVideo,
        "aria-label": `Video ${props.player.contentId || row.contentId}`,
        controls: true,
        playsInline: true,
        muted: props.player.muted,
        src: props.player.mediaUrl,
      });
  return createElement(
    "div",
    { className: "player" },
    video,
    props.player.captionsOn && props.player.captionText
      ? createElement("div", { className: "caption", role: "status", "aria-live": "polite" }, props.player.captionText)
      : null,
    ...props.player.sentences.map((sentence) => createElement("p", { key: sentence }, sentence)),
    createElement(
      "div",
      { className: "controls" },
      createElement("button", { type: "button", onClick: props.onTogglePlay }, props.player.playing ? "Pause" : "Play"),
      createElement("button", { type: "button", id: "replay", onClick: props.onReplay }, "Play again"),
      createElement("button", { type: "button", onClick: props.onToggleMute }, props.player.muted ? "Unmute" : "Mute"),
      createElement("button", { type: "button", onClick: props.onToggleCaptions }, props.player.captionsOn ? "Hide captions" : "Show captions"),
      createElement("button", { type: "button", onClick: () => props.onActive(Math.max(0, props.active - 1)) }, "Previous video"),
      createElement("button", { type: "button", onClick: () => props.onActive(Math.min(props.rows.length - 1, props.active + 1)) }, "Next video"),
      ...props.player.qualities.map((choice) =>
        createElement(
          "button",
          {
            type: "button",
            key: choice.id,
            "data-quality": choice.id,
            "aria-pressed": choice.current,
            onClick: () => props.onQuality(choice.id),
          },
          choice.label,
        ),
      ),
    ),
    gates(),
    createElement("p", null, "A response is not a completed watch."),
    props.player.details ? createElement("pre", { id: "svn-playback" }, props.player.details) : createElement("pre", { id: "svn-playback" }, ""),
  );
}

function gates(): ReactElement {
  return createElement(
    "div",
    { className: "gates" },
    gate("Follow", "gate-follow", "Follow is not available. No follow request is sent."),
    gate("Block", "gate-block", "Block is not available. No block request is sent."),
    gate("Report", "gate-report", "Report is not available. No report request is sent."),
    createElement("p", { id: "gate-comments" }, "Comments are not available."),
    createElement("p", null, "Peer sharing is off."),
  );
}

function gate(name: string, id: string, text: string): ReactElement {
  return createElement(
    "div",
    { className: "gate" },
    createElement("button", { type: "button", disabled: true, "aria-describedby": id }, name),
    createElement("p", { id }, text),
  );
}

function discover(props: ViewerProps): ReactElement {
  return createElement(
    "div",
    null,
    createElement("h1", null, "Discover"),
    createElement("p", null, "Committed content records. This is not a social search."),
    createElement("label", null, "Search", createElement("input", { disabled: true, "aria-describedby": "gate-search" })),
    createElement("p", { id: "gate-search" }, "Search is not available."),
    createElement(
      "div",
      { className: "list" },
      ...props.rows.map((row, index) =>
        row.withdrawn
          ? createElement("p", { key: row.contentId }, `${row.contentId} is withdrawn. Playback is not requested.`)
          : createElement(
              "button",
              { type: "button", key: row.contentId, onClick: () => { props.onActive(index); props.onPage("home"); } },
              `Open ${row.contentId}`,
            ),
      ),
    ),
  );
}

function activity(): ReactElement {
  return createElement(
    "div",
    null,
    createElement("h1", null, "Activity"),
    createElement("p", { className: "panel" }, "Replies and notifications are not available."),
  );
}

function profile(props: ViewerProps): ReactElement {
  return createElement(
    "div",
    null,
    createElement("h1", null, "Profile"),
    createElement(
      "p",
      null,
      props.session.signedIn
        ? `Account ${props.session.accountId}. Role ${props.session.role}. This id is an account record. It is not a public display name.`
        : props.session.message,
    ),
    createElement("button", { type: "button", onClick: props.onSignIn }, "Sign in with a passkey"),
    createElement(
      "form",
      {
        onSubmit: (event: { preventDefault: () => void; currentTarget: HTMLFormElement }) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          const invite = String(data.get("invite") ?? "");
          props.onEnroll(invite);
        },
      },
      createElement("label", null, "Invite", createElement("input", { name: "invite", autoComplete: "off" })),
      createElement("button", { type: "button", onClick: (event: { currentTarget: HTMLButtonElement }) => {
        const form = event.currentTarget.form;
        const invite = form ? String(new FormData(form).get("invite") ?? "") : "";
        props.onEnroll(invite);
      } }, "Enroll with this invite"),
    ),
    createElement("p", { className: "panel" }, "Account recovery is not available."),
    createElement("button", { type: "button", onClick: props.onExport, disabled: !props.session.signedIn }, "Export account"),
    createElement("button", { type: "button", onClick: props.onBackup, disabled: !props.session.signedIn }, "Create encrypted backup"),
    createElement(
      "form",
      {
        onSubmit: (event: { preventDefault: () => void }) => event.preventDefault(),
      },
      createElement("label", null, "Type delete to confirm", createElement("input", { name: "confirm", autoComplete: "off" })),
      createElement("button", {
        type: "button",
        disabled: !props.session.signedIn,
        onClick: (event: { currentTarget: HTMLButtonElement }) => {
          const form = event.currentTarget.form;
          props.onDelete(form ? String(new FormData(form).get("confirm") ?? "") : "");
        },
      }, "Delete account"),
    ),
    createElement("button", { type: "button", onClick: props.onSave, disabled: !props.session.signedIn }, "Save this video"),
    props.actionMessage ? createElement("p", { role: "status" }, props.actionMessage) : null,
    createElement("h2", null, "Reads"),
    ...props.readLines.map((line) => createElement("p", { key: line }, line)),
    createElement(
      "p",
      null,
      "The configured read gateway is trusted for reads. Two HTTP responses are not light-client verification.",
    ),
    createElement("p", null, EVIDENCE_LABELS.acceptance.text),
    createElement("p", null, "No attestation is shown for this video. A recorded attestation is not proof of storage."),
    settingsForm(props),
    createElement(
      "label",
      null,
      createElement("input", {
        type: "checkbox",
        checked: props.reducedMotion,
        onChange: (event: { currentTarget: HTMLInputElement }) => props.onReducedMotion(event.currentTarget.checked),
      }),
      "Reduce motion",
    ),
  );
}

function settingsForm(props: ViewerProps): ReactElement {
  const fields: [keyof ClientSettings, string][] = [
    ["deliveryBase", "Delivery"],
    ["indexerBase", "Indexer"],
    ["indexerBase2", "Second indexer"],
    ["accountBase", "Account"],
  ];
  return createElement(
    "form",
    {
      className: "panel",
      onSubmit: (event: { preventDefault: () => void; currentTarget: HTMLFormElement }) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        props.onSettings({
          deliveryBase: String(data.get("deliveryBase") ?? ""),
          indexerBase: String(data.get("indexerBase") ?? ""),
          indexerBase2: String(data.get("indexerBase2") ?? ""),
          accountBase: String(data.get("accountBase") ?? ""),
          contentIds: String(data.get("contentIds") ?? "")
            .split(/[\s,]+/)
            .filter((item) => item.length > 0),
        });
      },
    },
    createElement("h2", null, "Endpoints"),
    ...fields.map(([name, labelText]) =>
      createElement(
        "label",
        { key: name },
        labelText,
        createElement("input", { name, defaultValue: props.settings[name] }),
      ),
    ),
    createElement("label", null, "Content ids", createElement("input", { name: "contentIds", defaultValue: props.settings.contentIds.join(" ") })),
    createElement("button", { type: "submit" }, "Save endpoints"),
  );
}

function onFeedKey(event: KeyboardEvent<HTMLDivElement>, props: ViewerProps): void {
  const target = event.target;
  const inField = target instanceof HTMLElement && target.closest("button, input, textarea, a");
  if (event.key === "ArrowDown") {
    event.preventDefault();
    props.onActive(Math.min(props.rows.length - 1, props.active + 1));
  } else if (event.key === "ArrowUp") {
    event.preventDefault();
    props.onActive(Math.max(0, props.active - 1));
  } else if (!inField && (event.key === " " || event.key === "Spacebar")) {
    event.preventDefault();
    props.onTogglePlay();
  } else if (!inField && (event.key === "m" || event.key === "M")) {
    props.onToggleMute();
  } else if (!inField && (event.key === "c" || event.key === "C")) {
    props.onToggleCaptions();
  }
}

function label(page: PageName): string {
  if (page === "home") return "Home";
  if (page === "discover") return "Discover";
  if (page === "activity") return "Activity";
  return "Profile";
}

export function attachVideo(node: HTMLVideoElement | null, onReady: (video: HTMLVideoElement) => void): void {
  if (!node) return;
  const ready = () => onReady(node);
  node.addEventListener("loadeddata", ready);
  void node.play().catch(() => undefined);
}

export function useVideoReady(mediaUrl: string, onReady: (video: HTMLVideoElement) => void): (node: HTMLVideoElement | null) => void {
  const saved = useRef(onReady);
  saved.current = onReady;
  const nodeRef = useRef<HTMLVideoElement | null>(null);
  useEffect(() => {
    const node = nodeRef.current;
    if (!node || !mediaUrl) return;
    const ready = () => saved.current(node);
    if (node.readyState >= 2) ready();
    node.addEventListener("loadeddata", ready);
    void node.play().catch(() => undefined);
    return () => node.removeEventListener("loadeddata", ready);
  }, [mediaUrl]);
  return (node) => {
    nodeRef.current = node;
  };
}

export function textNode(value: string): ReactNode {
  return value;
}
