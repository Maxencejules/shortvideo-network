/**
 * Operator page. The session bearer stays in memory. This file has no
 * consensus key field and it does not put a token in the URL.
 */

import { DUTIES, readingText, storageForcesValidator, toggleDuty, visibleActions, type Reading } from "./operator.ts";

interface Proposal {
  id: string;
  state: string;
  approvals: number;
  approval_target: number;
  activation_height: number;
  execute_after_unix: number;
}

const PHRASE: Record<string, string> = {
  limits: "CHANGE LIMITS",
  "stop-offers": "STOP NEW OFFERS",
  drain: "DRAIN",
  handoff: "HANDOFF",
  renewal: "REVIEW RENEWAL",
  disable: "DISABLE ENDPOINT",
};

export function mount(root: HTMLElement, publicBase: string): void {
  let selected: string[] = [];
  let token = "";
  let role = "";
  const status = el("section");
  status.id = "status";
  const onboard = el("section");
  const sessions = el("section");
  const roleLine = el("p");
  roleLine.id = "role";
  const actions = el("section");
  actions.id = "actions";
  const council = el("section");
  council.id = "council";
  const queue = el("section");
  queue.id = "queue";
  const result = el("pre");
  result.id = "result";
  root.replaceChildren(status, onboard, sessions, roleLine, actions, council, queue, result);

  const boxes = new Map<string, HTMLInputElement>();
  for (const duty of DUTIES) {
    const box = document.createElement("input");
    box.type = "checkbox";
    box.id = dutyId(duty);
    box.addEventListener("change", () => {
      selected = toggleDuty(selected, duty);
      box.checked = selected.includes(duty);
    });
    const label = el("label");
    label.append(box, document.createTextNode(" " + duty));
    onboard.append(label);
    boxes.set(duty, box);
  }
  const labelInput = document.createElement("input");
  labelInput.id = "operator-label";
  labelInput.value = "demo";
  const domainInput = document.createElement("input");
  domainInput.id = "failure-domain";
  domainInput.value = "single-machine demo";
  const onboardButton = document.createElement("button");
  onboardButton.type = "button";
  onboardButton.id = "onboard";
  onboardButton.textContent = "Record onboarding";
  const onboardResult = el("p");
  onboardResult.id = "onboard-result";
  onboard.append(labelInput, domainInput, onboardButton, onboardResult);
  onboardButton.addEventListener("click", () => {
    void send("/v1/onboarding", {
      roles: selected,
      operator_label: labelInput.value,
      failure_domain: domainInput.value,
    }).then((doc) => {
      const forces = storageForcesValidator(selected);
      onboardResult.textContent = `storage does not force validator (${String(forces)}). validator_selected ${String(doc["validator_selected"])}. single-machine demo.`;
      result.textContent = JSON.stringify(doc);
    });
  });

  for (const name of ["viewer", "provider", "moderator", "council"]) {
    const button = document.createElement("button");
    button.type = "button";
    button.id = "session-" + name;
    button.textContent = "Development session " + name;
    button.addEventListener("click", () => {
      void postJSON("/v1/dev/session", { role: name, account_id: name + "1" }, "").then((doc) => {
        token = typeof doc["token"] === "string" ? doc["token"] : "";
        role = name;
        roleLine.textContent = "role " + name;
        drawActions();
        if (name === "moderator") void loadQueue();
      });
    });
    sessions.append(button);
  }

  function drawActions(): void {
    actions.replaceChildren();
    for (const name of visibleActions(role)) {
      const button = document.createElement("button");
      button.type = "button";
      button.id = "action-" + name;
      button.textContent = name;
      button.addEventListener("click", () => {
        if (name === "queue") {
          void loadQueue();
          return;
        }
        if (name === "governance") {
          void send("/v1/governance", {}).then((doc) => {
            result.textContent = JSON.stringify(doc);
          });
          return;
        }
        const body: Record<string, unknown> = { confirm: PHRASE[name] ?? "" };
        if (name === "limits") {
          body["quota_bytes"] = 1;
          body["bandwidth_bytes_per_second"] = 1;
        }
        if (name === "renewal") {
          body["lease_id"] = "lshare";
          body["decision"] = "hold";
        }
        void send("/v1/" + actionPath(name), body).then((doc) => {
          result.textContent = JSON.stringify(doc);
        });
      });
      actions.append(button);
    }
  }

  async function loadQueue(): Promise<void> {
    const headers: Record<string, string> = { accept: "application/json" };
    if (token) headers["authorization"] = "Bearer " + token;
    const res = await fetch("/v1/moderation/queue", { headers });
    queue.textContent = await res.text();
  }

  async function send(path: string, body: Record<string, unknown>): Promise<Record<string, unknown>> {
    return postJSON(path, body, token);
  }

  void loadPublic(publicBase, status, council);
}

async function loadPublic(base: string, status: HTMLElement, council: HTMLElement): Promise<void> {
  if (base === "") return;
  const snap = await fetch(base + "/v1/status").then((res) => res.json() as Promise<{ readings?: Reading[] }>);
  status.replaceChildren();
  for (const row of snap.readings ?? []) {
    const line = el("p");
    line.textContent = readingText(row);
    status.append(line);
  }
  const doc = await fetch(base + "/v1/council").then((res) => res.json() as Promise<Record<string, Reading>>);
  const proposals = doc["proposals"];
  const lines = [readingText(doc["upgrade_instructions"] ?? { name: "upgrade_instructions", class: "absent", value: null, source: "" })];
  const rows = Array.isArray(proposals?.value) ? (proposals.value as Proposal[]) : [];
  for (const row of rows) {
    lines.push(`proposal ${row.id} state ${row.state} approvals ${String(row.approvals)} of ${String(row.approval_target)} activation_height ${String(row.activation_height)}`);
  }
  if (proposals) lines.push(readingText(proposals));
  council.textContent = lines.join("\n");
}

function actionPath(name: string): string {
  if (name === "renewal") return "renewals/review";
  if (name === "disable") return "endpoints/disable";
  return name;
}

async function postJSON(path: string, body: Record<string, unknown>, token: string): Promise<Record<string, unknown>> {
  const headers: Record<string, string> = { "content-type": "application/json", accept: "application/json" };
  if (token) headers["authorization"] = "Bearer " + token;
  const res = await fetch(path, { method: "POST", headers, body: JSON.stringify(body) });
  return (await res.json()) as Record<string, unknown>;
}

function dutyId(duty: string): string {
  if (duty === "storage_delivery") return "duty-storage";
  if (duty === "account_discovery") return "duty-account";
  return "duty-" + duty;
}

function el(tag: string): HTMLElement {
  return document.createElement(tag);
}

const root = document.querySelector("#operator");
if (root instanceof HTMLElement) {
  const pub = document.querySelector('meta[name="svn-public"]')?.getAttribute("content") ?? "";
  mount(root, pub);
}
