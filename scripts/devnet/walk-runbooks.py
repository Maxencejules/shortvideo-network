#!/usr/bin/env python3
"""Walk the goal-10 operator runbooks on a disposable single-machine devnet.

Subcommands:

    walk-runbooks.py list
        Print every block tagged walk:<id> in the walked runbooks, in order.
    walk-runbooks.py walk [--work DIR] [--keep]
        Build svnd from this checkout, start a four-process devnet in a new
        work directory, run every tagged block verbatim with bash, in order,
        and print "block <id> exit <code>" for each. Exit 1 when a block
        fails or is not run, or when the extra refusal check fails.
    walk-runbooks.py query {policy,governance} --rpc URL [--height N]
        Read svn.policy.v1.Query/Policy or svn.authority.v1.Query/Governance
        through CometBFT abci_query and print the height read and the
        response as protobuf JSON. svnd has no query subcommand; the runbooks
        use this.

The walk binds the devnet's fixed loopback ports, so run it inside a
private network namespace (for example `unshare --net` with loopback up)
when another devnet may run on the machine. Go modules must already be in
the module cache: builds run with GOPROXY=off.

What a walk shows: the tagged shell steps run as written against a
single-machine four-process devnet (one operator, not independent
operators). The release binary is a rebuild of this checkout that stands in
for a reviewed release, and the manifest is written by this script. The
state check binary (CHECK_BIN) is a -tags svn_statecheck build of the same
checkout. No upgrade plan is scheduled (svnd has no transaction
subcommand), so the walk does not produce a halt or a divergence; the
upgrade itself is exercised by the Go rehearsal tests. After the blocks, one
extra check starts a rehearsal build (-tags svn_rehearsal) on a stopped node
and expects the load gate to refuse it.
"""

from __future__ import annotations

import argparse
import base64
import importlib
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from types import FrameType
from typing import Any

ROOT = Path(__file__).resolve().parents[2]

# The runbooks whose bash blocks all carry a walk:<id> tag, in walk order.
WALKED = (
    "docs/operations/operator-installation.md",
    "docs/operations/planned-halt-and-restart.md",
    "docs/operations/failed-upgrade.md",
)

FENCE = re.compile(r"^( {0,3})```(.*)$")
WALK_TAG = re.compile(r"^walk:([a-z0-9][a-z0-9-]*)$")
BLOCK_TIMEOUT_SECONDS = 300
START_TIMEOUT_SECONDS = 120
REFUSAL_TIMEOUT_SECONDS = 60

QUERIES = {
    "policy": (
        "/svn.policy.v1.Query/Policy",
        "svn.policy.v1.query_pb2",
        "QueryPolicyRequest",
        "QueryPolicyResponse",
    ),
    "governance": (
        "/svn.authority.v1.Query/Governance",
        "svn.authority.v1.query_pb2",
        "QueryGovernanceRequest",
        "QueryGovernanceResponse",
    ),
}


@dataclass(frozen=True)
class Fenced:
    """One fenced code block of a Markdown file."""

    source: str
    line: int
    info: str
    text: str


@dataclass(frozen=True)
class Block:
    """One bash block tagged walk:<id>."""

    source: str
    line: int
    walk_id: str
    text: str


def fenced_blocks(path: Path, source: str) -> list[Fenced]:
    """Return the fenced code blocks of a Markdown file, dedented by the fence indent."""
    out: list[Fenced] = []
    lines = path.read_text(encoding="utf-8").splitlines()
    index = 0
    while index < len(lines):
        opening = FENCE.match(lines[index])
        if opening is None:
            index += 1
            continue
        indent = len(opening.group(1))
        info = opening.group(2).strip()
        body: list[str] = []
        end = index + 1
        while end < len(lines):
            closing = FENCE.match(lines[end])
            if closing is not None and closing.group(2).strip() == "":
                break
            body.append(lines[end][indent:] if lines[end][:indent].strip() == "" else lines[end])
            end += 1
        if end == len(lines):
            raise ValueError(f"{source}:{index + 1}: code fence is not closed")
        out.append(Fenced(source, index + 1, info, "\n".join(body) + "\n"))
        index = end + 1
    return out


def walk_blocks(root: Path = ROOT) -> list[Block]:
    """Return the tagged bash blocks of the walked runbooks, in walk order.

    Every bash block of a walked runbook must carry exactly one walk:<id>
    tag, and ids are unique across the runbooks.
    """
    blocks: list[Block] = []
    seen: dict[str, str] = {}
    for source in WALKED:
        for fenced in fenced_blocks(root / source, source):
            words = fenced.info.split()
            if not words or words[0] != "bash":
                continue
            tags = [WALK_TAG.match(word) for word in words[1:]]
            ids = [tag.group(1) for tag in tags if tag is not None]
            if len(ids) != 1 or len(words) != 2:
                raise ValueError(f"{source}:{fenced.line}: a bash block needs one walk:<id> tag")
            walk_id = ids[0]
            if walk_id in seen:
                raise ValueError(
                    f"{source}:{fenced.line}: walk id {walk_id} repeats {seen[walk_id]}"
                )
            seen[walk_id] = f"{source}:{fenced.line}"
            blocks.append(Block(source, fenced.line, walk_id, fenced.text))
    if not blocks:
        raise ValueError("the walked runbooks hold no tagged block")
    return blocks


def no_proxy_opener() -> urllib.request.OpenerDirector:
    """An opener that never uses a proxy: the devnet RPC is on loopback."""
    return urllib.request.build_opener(urllib.request.ProxyHandler({}))


def abci_query(rpc: str, path: str, data: bytes, height: int) -> tuple[int, bytes]:
    """Run CometBFT abci_query and return the height read and the response bytes."""
    params = {"path": json.dumps(path)}
    if data:
        params["data"] = "0x" + data.hex()
    if height > 0:
        params["height"] = str(height)
    url = rpc.rstrip("/") + "/abci_query?" + urllib.parse.urlencode(params)
    with no_proxy_opener().open(url, timeout=10) as response:
        body = json.load(response)
    if "error" in body:
        raise RuntimeError(f"abci_query {path}: {body['error']}")
    result = body["result"]["response"]
    code = int(result.get("code", 0))
    if code != 0:
        raise RuntimeError(
            f"abci_query {path}: codespace {result.get('codespace', '')} code {code}: "
            f"{result.get('log', '')}"
        )
    return int(result.get("height", "0")), base64.b64decode(result.get("value") or "")


def run_query(kind: str, rpc: str, height: int) -> int:
    """Print the height and the protobuf JSON of one query response."""
    path, module_name, request_name, response_name = QUERIES[kind]
    generated = str(ROOT / "proto" / "gen" / "python")
    if generated not in sys.path:
        sys.path.insert(0, generated)
    module: Any = importlib.import_module(module_name)
    json_format: Any = importlib.import_module("google.protobuf.json_format")
    request = getattr(module, request_name)()
    read_height, value = abci_query(rpc, path, request.SerializeToString(), height)
    message = getattr(module, response_name)()
    message.ParseFromString(value)
    print(f"height {read_height}")
    print(json_format.MessageToJson(message, sort_keys=True))
    return 0


def tool_env() -> dict[str, str]:
    """The environment of every command the walk runs: no Go module download."""
    env = dict(os.environ)
    env["GOPROXY"] = "off"
    return env


def build(out: Path, tags: str = "") -> str:
    """Build svnd from this checkout into out and return its sha256."""
    command = ["go", "build", "-C", str(ROOT / "chain"), "-trimpath"]
    if tags:
        command += ["-tags", tags]
    command += ["-o", str(out), "./cmd/svnd"]
    print("+ " + " ".join(command), flush=True)
    subprocess.run(command, check=True, env=tool_env(), stdin=subprocess.DEVNULL)
    digest = subprocess.run(
        ["sha256sum", str(out)], check=True, capture_output=True, text=True
    ).stdout.split()[0]
    print(f"built {out.name} sha256 {digest}", flush=True)
    return digest


def node_height(binary: Path, rpc: str) -> int | None:
    """The latest committed height svnd status reads, or None."""
    completed = subprocess.run(
        [str(binary), "status", "--rpc", rpc],
        capture_output=True,
        text=True,
        stdin=subprocess.DEVNULL,
        env=tool_env(),
        timeout=20,
    )
    if completed.returncode != 0:
        return None
    for line in completed.stdout.splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[0] == "height":
            return int(fields[1])
    return None


def wait_heights(binary: Path, rpcs: list[str], minimum: int) -> None:
    """Wait until every node reports a committed height of at least minimum."""
    deadline = time.monotonic() + START_TIMEOUT_SECONDS
    heights: list[int | None] = []
    while time.monotonic() < deadline:
        heights = [node_height(binary, rpc) for rpc in rpcs]
        if all(height is not None and height >= minimum for height in heights):
            print(f"devnet heights {heights}", flush=True)
            return
        time.sleep(1)
    raise RuntimeError(f"the devnet did not reach height {minimum}: {heights}")


def stop_pid(pidfile: Path) -> None:
    """Stop the process of a pidfile with SIGTERM and wait until it exits."""
    pid = int(pidfile.read_text(encoding="utf-8").strip())
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return
        time.sleep(0.5)
    raise RuntimeError(f"pid {pid} of {pidfile} did not stop")


def run_block(block: Block, env: dict[str, str]) -> int:
    """Run one block verbatim with bash -euo pipefail and print its output."""
    print(f"== block {block.walk_id} ({block.source}:{block.line})", flush=True)
    try:
        completed = subprocess.run(
            ["bash", "-euo", "pipefail", "-c", block.text],
            cwd=ROOT,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=BLOCK_TIMEOUT_SECONDS,
        )
    except subprocess.TimeoutExpired as exc:
        output = exc.stdout if isinstance(exc.stdout, str) else ""
        sys.stdout.write("".join(f"   | {line}\n" for line in output.splitlines()))
        print(f"block {block.walk_id} exit timeout after {BLOCK_TIMEOUT_SECONDS} s", flush=True)
        return 124
    sys.stdout.write("".join(f"   | {line}\n" for line in completed.stdout.splitlines()))
    print(f"block {block.walk_id} exit {completed.returncode}", flush=True)
    return completed.returncode


def refusal_check(wrong: Path, node: Path) -> bool:
    """Start a rehearsal build on a stopped node and expect a gate refusal."""
    stop_pid(node / "svnd.pid")
    print(f"== check wrong-binary-refused ({wrong.name} start --home {node})", flush=True)
    completed = subprocess.run(
        [str(wrong), "start", "--home", str(node)],
        capture_output=True,
        text=True,
        stdin=subprocess.DEVNULL,
        env=tool_env(),
        timeout=REFUSAL_TIMEOUT_SECONDS,
    )
    output = (completed.stdout + completed.stderr).strip().splitlines()
    last = output[-1] if output else ""
    print(f"   | {last}")
    refused = (
        completed.returncode == 1
        and "load gate at height" in last
        and "(row 6)" in last
        and "no upgrade plan is stored" in last
    )
    print(f"check wrong-binary-refused exit {completed.returncode} expected 1", flush=True)
    return refused


def walk(work: Path, keep: bool) -> int:
    """Run every tagged block against a new devnet in work."""
    blocks = walk_blocks()
    for tool in ("go", "bash", "curl", "jq", "sha256sum"):
        if shutil.which(tool) is None:
            print(f"missing tool {tool}", file=sys.stderr)
            return 1
    if not (ROOT / ".venv" / "bin" / "python").is_file():
        print("missing .venv/bin/python (the query blocks use it)", file=sys.stderr)
        return 1
    print("NOTE single-machine four-process devnet: one operator, not independent operators")
    print(
        "NOTE the release binary is a rebuild of this checkout standing in for a reviewed release"
    )
    print("NOTE no upgrade plan is scheduled; the walk shows no halt and no divergence")
    print(f"work {work}", flush=True)
    devnet_home = work / "devnet"
    running = work / "bin" / "svnd"
    release = work / "release" / "svnd"
    wrong = work / "bin" / "svnd-rehearsal"
    check = work / "bin" / "svnd-statecheck"
    results: dict[str, int] = {}
    refused = False
    started = False
    try:
        build(running)
        digest = build(release)
        (work / "release" / "svnd.sha256").write_text(f"{digest}  svnd\n", encoding="utf-8")
        build(wrong, tags="svn_rehearsal")
        build(check, tags="svn_statecheck")
        env = tool_env()
        env["SVN_HOME"] = str(devnet_home)
        env["SVN_BIN"] = str(running)
        started = True
        subprocess.run(
            ["bash", str(ROOT / "scripts" / "devnet" / "up.sh")],
            check=True,
            cwd=ROOT,
            env=env,
            stdin=subprocess.DEVNULL,
        )
        topology = json.loads((devnet_home / "topology.json").read_text(encoding="utf-8"))
        nodes = {node["name"]: node for node in topology["validators"]}
        wait_heights(running, [node["rpc"] for node in topology["validators"]], 3)
        env.update(
            {
                "SVN_SRC": str(ROOT),
                "NODE": str(devnet_home / "nodes" / "v4"),
                "NODE_RPC": nodes["v4"]["rpc"],
                "PEER_RPC": nodes["v1"]["rpc"],
                "NEW_BIN": str(release),
                "MANIFEST": str(work / "release" / "svnd.sha256"),
                "INSTALL_DIR": str(work / "install" / "v4"),
                "NODE_COPY": str(work / "copy-v4"),
                "NOTES": str(work / "notes"),
                "CHECK_BIN": str(check),
            }
        )
        for block in blocks:
            results[block.walk_id] = run_block(block, env)
            if results[block.walk_id] != 0:
                break
        if all(results.get(block.walk_id) == 0 for block in blocks):
            refused = refusal_check(wrong, devnet_home / "nodes" / "v4")
    except (OSError, RuntimeError, ValueError, KeyError, subprocess.SubprocessError) as exc:
        print(f"walk stopped: {exc}", flush=True)
    finally:
        if started and (devnet_home / "nodes").is_dir():
            down = tool_env()
            down["SVN_HOME"] = str(devnet_home)
            subprocess.run(
                ["bash", str(ROOT / "scripts" / "devnet" / "down.sh")],
                cwd=ROOT,
                env=down,
                stdin=subprocess.DEVNULL,
                check=False,
            )
        if keep:
            print(f"kept {work}")
        else:
            shutil.rmtree(work, ignore_errors=True)
    failed = False
    for block in blocks:
        code = results.get(block.walk_id)
        if code is None:
            print(f"block {block.walk_id} not run")
            failed = True
        elif code != 0:
            failed = True
    if not refused:
        print("check wrong-binary-refused FAILED or not run")
        failed = True
    passed = sum(1 for code in results.values() if code == 0)
    print(f"blocks {len(blocks)} run {len(results)} passed {passed}")
    print("result", "FAIL" if failed else "PASS")
    return 1 if failed else 0


def terminate(signum: int, _frame: FrameType | None) -> None:
    """Turn SIGTERM into an exit, so the walk stops its devnet."""
    raise SystemExit(128 + signum)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0] if __doc__ else None)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("list", help="print the tagged blocks in walk order")
    walk_parser = sub.add_parser("walk", help="run the tagged blocks against a new devnet")
    walk_parser.add_argument("--work", type=Path, help="new work directory (default: a temp dir)")
    walk_parser.add_argument("--keep", action="store_true", help="keep the work directory")
    query_parser = sub.add_parser("query", help="read Query/Policy or Query/Governance")
    query_parser.add_argument("kind", choices=sorted(QUERIES))
    query_parser.add_argument("--rpc", required=True, help="CometBFT RPC base URL")
    query_parser.add_argument("--height", type=int, default=0, help="committed height (0: latest)")
    args = parser.parse_args(argv)
    if args.command == "list":
        for block in walk_blocks():
            print(f"== block {block.walk_id} ({block.source}:{block.line})")
            sys.stdout.write(block.text)
        return 0
    if args.command == "query":
        try:
            return run_query(args.kind, args.rpc, args.height)
        except (OSError, RuntimeError, ValueError) as exc:
            print(f"query {args.kind}: {exc}", file=sys.stderr)
            return 1
    signal.signal(signal.SIGTERM, terminate)
    if args.work is None:
        work = Path(tempfile.mkdtemp(prefix="svn-walk-"))
    else:
        work = args.work.resolve()
        if work.exists() and any(work.iterdir()):
            print(f"refusing {work}: the work directory must be new or empty", file=sys.stderr)
            return 2
        work.mkdir(parents=True, exist_ok=True)
    return walk(work, args.keep)


if __name__ == "__main__":
    sys.exit(main())
