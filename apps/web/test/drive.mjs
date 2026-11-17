import { spawn } from "node:child_process";
import { readFileSync, writeSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";

const plan = JSON.parse(readFileSync(process.argv[2] ?? "", "utf8"));
const saved = {};
const profile = await mkdtemp(join(tmpdir(), "svn-web-"));
const chrome = spawn(plan.chrome, [
  "--headless=new",
  "--disable-gpu",
  "--no-sandbox",
  "--disable-dev-shm-usage",
  "--ignore-certificate-errors",
  "--allow-insecure-localhost",
  "--autoplay-policy=no-user-gesture-required",
  "--no-first-run",
  "--no-default-browser-check",
  `--user-data-dir=${profile}`,
  "--window-size=1280,800",
  "--remote-debugging-port=0",
  "about:blank",
], { detached: true, stdio: ["ignore", "ignore", "pipe"] });

try {
  const port = await debuggingPort(chrome);
  const version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
  const list = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
  const page = list.find((item) => item.type === "page" && item.webSocketDebuggerUrl);
  if (!page) throw new Error("chrome opened no page");
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    ws.addEventListener("open", resolve);
    ws.addEventListener("error", reject);
  });
  const send = cdp(ws);
  await send("Runtime.enable");
  await send("Page.enable");
  await send("DOM.enable");
  const browser = {
    browser: version.Browser ?? "",
    userAgent: version["User-Agent"] ?? "",
    protocol: version["Protocol-Version"] ?? "",
  };
  for (const step of plan.steps ?? []) {
    if (step.do === "goto") {
      await send("Page.navigate", { url: step.url });
      await sleep(400);
    } else if (step.do === "viewport") {
      await send("Emulation.setDeviceMetricsOverride", {
        width: step.width,
        height: step.height,
        deviceScaleFactor: step.mobile ? 2 : 1,
        mobile: Boolean(step.mobile),
      });
    } else if (step.do === "motion") {
      await send("Emulation.setEmulatedMedia", {
        features: [{ name: "prefers-reduced-motion", value: step.value }],
      });
    } else if (step.do === "reload") {
      await send("Page.reload", { ignoreCache: true });
      await sleep(500);
    } else if (step.do === "wait") {
      try {
        saved[step.save ?? "last"] = await waitFor(send, step.expr, step.ms ?? 20000);
      } catch (error) {
        throw new Error(`${step.save ?? "wait"}: ${error.message}`);
      }
    } else if (step.do === "eval") {
      saved[step.save ?? "last"] = await evaluate(send, step.expr);
    } else if (step.do === "click") {
      const box = await evaluate(send, `(() => { const node = document.querySelector(${JSON.stringify(step.selector)}); if (!node) return null; node.scrollIntoView({block: "center", inline: "center"}); const rect = node.getBoundingClientRect(); return { x: rect.x + Math.min(rect.width / 2, 20), y: rect.y + Math.min(rect.height / 2, 12) }; })()`);
      if (!box) throw new Error(`missing ${step.selector}`);
      await send("Input.dispatchMouseEvent", { type: "mousePressed", x: box.x, y: box.y, button: "left", clickCount: 1 });
      await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: box.x, y: box.y, button: "left", clickCount: 1 });
    } else if (step.do === "key") {
      await send("Input.dispatchKeyEvent", { type: "keyDown", key: step.key, code: step.code, windowsVirtualKeyCode: step.codePoint ?? 0 });
      await send("Input.dispatchKeyEvent", { type: "keyUp", key: step.key, code: step.code, windowsVirtualKeyCode: step.codePoint ?? 0 });
    } else if (step.do === "pause") {
      writeSync(1, `${JSON.stringify({ event: "pause", saved, browser })}\n`);
      await readLine();
    } else if (step.do === "sleep") {
      await sleep(step.ms ?? 200);
    } else if (step.do === "file") {
      const documentNode = await send("DOM.getDocument");
      const found = await send("DOM.querySelector", { nodeId: documentNode.root.nodeId, selector: step.selector });
      if (!found.nodeId) throw new Error(`missing ${step.selector}`);
      await send("DOM.setFileInputFiles", { files: [step.path], nodeId: found.nodeId });
    } else {
      throw new Error(`unknown step ${step.do}`);
    }
  }
  const pageAgent = await evaluate(send, "navigator.userAgent");
  const viewport = await evaluate(send, "({ width: window.innerWidth, height: window.innerHeight, dpr: window.devicePixelRatio })");
  writeSync(1, `${JSON.stringify({ event: "done", saved, browser, pageAgent, viewport })}\n`);
  ws.close();
} catch (error) {
  writeSync(1, `${JSON.stringify({ event: "error", message: String(error && error.stack || error).slice(0, 800) })}\n`);
  process.exitCode = 1;
} finally {
  if (chrome.pid) {
    try { process.kill(-chrome.pid, "SIGTERM"); } catch { chrome.kill("SIGTERM"); }
  }
  chrome.stderr.destroy();
  await sleep(200);
  await rm(profile, { recursive: true, force: true });
  process.exit(process.exitCode ?? 0);
}

function cdp(ws) {
  let seq = 0;
  const pending = new Map();
  ws.addEventListener("message", (event) => {
    const message = JSON.parse(event.data);
    const found = pending.get(message.id);
    if (!found) return;
    pending.delete(message.id);
    if (message.error) found.reject(new Error(JSON.stringify(message.error)));
    else found.resolve(message.result ?? {});
  });
  return (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++seq;
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params }));
  });
}

async function evaluate(send, expression) {
  const result = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails).slice(0, 500));
  return result.result?.value ?? null;
}

async function waitFor(send, expression, ms) {
  const deadline = Date.now() + ms;
  let last = null;
  while (Date.now() < deadline) {
    last = await evaluate(send, expression);
    if (last) return last;
    await sleep(200);
  }
  const page = await evaluate(send, "document.body ? document.body.innerText.slice(0, 700) : ''").catch(() => "");
  throw new Error(`timeout last ${JSON.stringify(last)} page ${String(page).slice(0, 700)}`);
}

async function debuggingPort(child) {
  return new Promise((resolve, reject) => {
    let buffer = "";
    const timer = setTimeout(() => reject(new Error("chrome did not open a debugging port")), 10000);
    child.stderr.on("data", (chunk) => {
      buffer += chunk.toString();
      const match = /DevTools listening on ws:\/\/127\.0\.0\.1:(\d+)\//.exec(buffer);
      if (match) {
        clearTimeout(timer);
        resolve(match[1]);
      }
    });
    child.on("exit", () => reject(new Error("chrome exited")));
  });
}

function readLine() {
  return new Promise((resolve) => {
    const lines = createInterface({ input: process.stdin });
    lines.once("line", (line) => {
      lines.close();
      resolve(line);
    });
  });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
