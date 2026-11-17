/**
 * Presentational viewer. User text is a React child, never HTML.
 */

import { createElement, useEffect, useRef, type KeyboardEvent, type ReactElement, type ReactNode } from "react";

import { EVIDENCE_LABELS } from "./evidence-labels.ts";
import { RECOVERY_TEXT, stageNames, type CreatorState } from "./creator.ts";
import type { ClientSettings, FeedRow } from "./model.ts";
import type { SocialHit, SocialNote } from "./social.ts";

export type PageName = "home" | "discover" | "activity" | "profile" | "create";

export type CreatorFields = {
  title: string;
  description: string;
  captions: string;
  trimStart: string;
  trimEnd: string;
  confirm: string;
};

export type CreatorPanel = {
  state: CreatorState;
  snapshot: string;
  fields: CreatorFields;
  fileName: string;
  message: string;
  terms: string[];
  analytics: string[];
  videos: { key: string; label: string; stage: string }[];
};

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
  creator: CreatorPanel;
  onCreatorField: (name: keyof CreatorFields, value: string) => void;
  onCreatorFile: (file: File | null) => void;
  onCreatorAction: (action: string) => void;
  social?: {
    feedKind: "following" | "recommended";
    factors: string[];
    comments: SocialNote[];
    activity: SocialNote[];
    hits: SocialHit[];
    onFeedKind: (kind: "following" | "recommended") => void;
    onFollow?: () => void;
    onLike?: () => void;
    onNotInterested?: () => void;
    onComment?: (text: string) => void;
    onSearch?: (kind: string, query: string) => void;
  };
};

const PAGES: PageName[] = ["home", "discover", "activity", "profile", "create"];

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
      props.page === "activity" ? activity(props) : null,
      props.page === "profile" ? profile(props) : null,
      props.page === "create" ? create(props) : null,
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
      createElement(
        "div",
        { className: "controls" },
        createElement("button", {
          type: "button",
          id: "feed-following",
          "aria-pressed": props.social?.feedKind === "following",
          onClick: () => props.social?.onFeedKind("following"),
        }, "Following"),
        createElement("button", {
          type: "button",
          id: "feed-recommended",
          "aria-pressed": props.social?.feedKind !== "following",
          onClick: () => props.social?.onFeedKind("recommended"),
        }, "Recommended"),
      ),
      props.social?.factors.length
        ? createElement("p", { id: "ranking-factors" }, `Ranking factors: ${props.social.factors.join(". ")}. This is a fixture rule, not a measured improvement.`)
        : null,
      props.actionMessage ? createElement("p", { id: "action-status", role: "status" }, props.actionMessage) : null,
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
    item.title ? createElement("p", null, item.title) : null,
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
      createElement(
        "button",
        { type: "button", id: "mute", onClick: props.onToggleMute },
        props.player.muted ? "Unmute" : "Mute",
      ),
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
    socialControls(props),
    createElement("p", null, "A response is not a completed watch."),
    props.player.details ? createElement("pre", { id: "svn-playback" }, props.player.details) : createElement("pre", { id: "svn-playback" }, ""),
  );
}

function socialControls(props: ViewerProps): ReactElement {
  const social = props.social;
  return createElement(
    "div",
    { className: "gates" },
    createElement("button", {
      type: "button",
      id: "follow",
      disabled: !social?.onFollow,
      onClick: () => social?.onFollow?.(),
    }, "Follow"),
    createElement("p", { id: "follow-note" }, "A follow is stored for this account. It is not a chain transaction."),
    createElement("button", { type: "button", id: "like", disabled: !social?.onLike, onClick: () => social?.onLike?.() }, "Like"),
    createElement("button", {
      type: "button",
      id: "not-interested",
      disabled: !social?.onNotInterested,
      onClick: () => social?.onNotInterested?.(),
    }, "Not interested"),
    createElement(
      "form",
      {
        onSubmit: (event: { preventDefault: () => void; currentTarget: HTMLFormElement }) => {
          event.preventDefault();
          social?.onComment?.(String(new FormData(event.currentTarget).get("text") ?? ""));
        },
      },
      createElement("label", null, "Comment", createElement("textarea", { id: "comment-text", name: "text", disabled: !social?.onComment })),
      createElement("button", { type: "submit", id: "comment-send", disabled: !social?.onComment }, "Comment"),
    ),
    createElement("p", { id: "comment-note" }, "Comments are plain text. A moderator queue is not implemented."),
    createElement("ul", { id: "comments" }, ...((social?.comments ?? []).map((note) => createElement("li", { key: note.id }, note.text)))),
    gate("Block", "gate-block", "Block is not available. No block request is sent."),
    gate("Report", "gate-report", "Report is not available. No report request is sent."),
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
  const social = props.social;
  return createElement(
    "div",
    null,
    createElement("h1", null, "Discover"),
    createElement("p", null, "Search reads permitted profiles, topics, and videos."),
    createElement(
      "form",
      {
        onSubmit: (event: { preventDefault: () => void; currentTarget: HTMLFormElement }) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          social?.onSearch?.(String(data.get("kind") ?? "video"), String(data.get("q") ?? ""));
        },
      },
      createElement("label", null, "Search", createElement("input", { id: "search-query", name: "q", disabled: !social?.onSearch })),
      createElement(
        "select",
        { id: "search-kind", name: "kind", defaultValue: "video", disabled: !social?.onSearch },
        createElement("option", { value: "video" }, "Videos"),
        createElement("option", { value: "profile" }, "Profiles"),
        createElement("option", { value: "topic" }, "Topics"),
      ),
      createElement("button", { type: "submit", id: "search-go", disabled: !social?.onSearch }, "Search"),
    ),
    createElement("ul", { id: "search-hits" }, ...((social?.hits ?? []).map((hit) => createElement("li", { key: `${hit.kind}-${hit.id}` }, `${hit.text} ${hit.id}`)))),
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

function activity(props: ViewerProps): ReactElement {
  const notes = props.social?.activity ?? [];
  return createElement(
    "div",
    null,
    createElement("h1", null, "Activity"),
    notes.length === 0
      ? createElement("p", { className: "panel" }, props.social ? "No replies or notifications." : "Set the discovery endpoint to load replies and notifications.")
      : createElement("ul", { id: "activity-notes" }, ...notes.map((note) => createElement("li", { key: note.id }, note.text))),
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
    createElement("p", { className: "panel" }, RECOVERY_TEXT),
    createElement("h2", null, "Your videos"),
    props.creator.videos.length === 0
      ? createElement("p", null, "No upload is stored in this browser.")
      : createElement(
          "ul",
          null,
          ...props.creator.videos.map((item) =>
            createElement("li", { key: item.key }, `${item.label}. ${item.stage}`),
          ),
        ),
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
    ["schedulerBase", "Scheduler"],
    ["discoveryBase", "Discovery"],
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
          schedulerBase: String(data.get("schedulerBase") ?? ""),
          discoveryBase: String(data.get("discoveryBase") ?? ""),
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

function create(props: ViewerProps): ReactElement {
  const panel = props.creator;
  const locked = !panel.state.canEdit;
  return createElement(
    "div",
    { className: "creator" },
    createElement("h1", null, "Create"),
    createElement(
      "ol",
      { className: "stages", "aria-label": "Creator stages" },
      ...stageNames().map((name) =>
        createElement("li", { key: name, "aria-current": panel.state.stage === name ? "step" : undefined }, name),
      ),
    ),
    createElement("p", { id: "creator-stage", role: "status", "aria-live": "polite" }, panel.state.stage),
    panel.state.progressMax
      ? createElement("progress", { max: panel.state.progressMax, value: panel.state.progressValue ?? 0 })
      : null,
    ...panel.state.notices.map((notice) => createElement("p", { key: notice }, notice)),
    createElement(
      "label",
      null,
      "Video file",
      createElement("input", {
        id: "video-file",
        type: "file",
        accept: "video/mp4,video/quicktime",
        disabled: locked,
        onChange: (event: { currentTarget: HTMLInputElement }) => {
          props.onCreatorFile(event.currentTarget.files?.[0] ?? null);
        },
      }),
    ),
    createElement("p", null, panel.fileName ? `Selected file ${panel.fileName}` : "No file is selected."),
    field(props, "trimStart", "Trim start, seconds"),
    field(props, "trimEnd", "Trim end, seconds"),
    field(props, "title", "Title"),
    textArea(props, "description", "Description"),
    textArea(props, "captions", "Captions"),
    createElement("p", null, "Thumbnail"),
    createElement(
      "label",
      null,
      createElement("input", { type: "radio", name: "thumbnail", checked: true, readOnly: true }),
      "Worker thumbnail",
    ),
    createElement(
      "label",
      null,
      createElement("input", { type: "radio", name: "thumbnail", disabled: true }),
      "Another frame",
    ),
    createElement("h2", null, "Hosting terms"),
    ...panel.terms.map((line) => createElement("p", { key: line }, line)),
    createElement("h2", null, "Analytics"),
    ...panel.analytics.map((line) => createElement("p", { key: line }, line)),
    createElement("p", { className: "panel" }, RECOVERY_TEXT),
    createElement("button", { type: "button", onClick: props.onExport, disabled: locked }, "Export account"),
    field(props, "confirm", "Confirmation word"),
    createElement("button", { type: "button", id: "creator-upload", onClick: () => props.onCreatorAction("upload"), disabled: locked }, "Upload"),
    createElement("button", { type: "button", id: "creator-refresh", onClick: () => props.onCreatorAction("refresh") }, "Refresh status"),
    createElement("button", { type: "button", id: "creator-retry", onClick: () => props.onCreatorAction("retry"), disabled: locked }, "Retry"),
    createElement("button", { type: "button", id: "creator-save", onClick: () => props.onCreatorAction("save") }, "Save draft"),
    createElement("button", { type: "button", id: "creator-version", onClick: () => props.onCreatorAction("version"), disabled: locked }, "New version"),
    createElement("button", { type: "button", id: "creator-cancel", onClick: () => props.onCreatorAction("cancel"), disabled: locked }, "Cancel upload"),
    createElement("button", { type: "button", id: "creator-publish", onClick: () => props.onCreatorAction("publish"), disabled: locked }, "Publication intent"),
    createElement("button", { type: "button", id: "creator-remove", onClick: () => props.onCreatorAction("remove"), disabled: locked }, "Removal request"),
    createElement("button", { type: "button", id: "creator-delete", onClick: () => props.onCreatorAction("delete") }, "Delete draft"),
    panel.message ? createElement("p", { id: "creator-message", role: "status" }, panel.message) : null,
    createElement("pre", { id: "svn-creator" }, panel.snapshot),
  );
}

function field(props: ViewerProps, name: keyof CreatorFields, labelText: string): ReactElement {
  return createElement(
    "label",
    null,
    labelText,
    createElement("input", {
      name,
      value: props.creator.fields[name],
      autoComplete: "off",
      onChange: (event: { currentTarget: HTMLInputElement }) => props.onCreatorField(name, event.currentTarget.value),
    }),
  );
}

function textArea(props: ViewerProps, name: keyof CreatorFields, labelText: string): ReactElement {
  return createElement(
    "label",
    null,
    labelText,
    createElement("textarea", {
      name,
      value: props.creator.fields[name],
      onChange: (event: { currentTarget: HTMLTextAreaElement }) => props.onCreatorField(name, event.currentTarget.value),
    }),
  );
}

function label(page: PageName): string {
  if (page === "home") return "Home";
  if (page === "discover") return "Discover";
  if (page === "activity") return "Activity";
  if (page === "create") return "Create";
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
