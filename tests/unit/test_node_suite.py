"""An empty Node test file is not a passing suite."""

from __future__ import annotations

import subprocess
from pathlib import Path

from scripts.run_tests import node_command, node_run_status, node_test_files, real_node_subtests


def test_named_subtest_counts() -> None:
    tap = (
        "# Subtest: react version is the pinned release\n"
        "ok 1 - react version is the pinned release\n"
    )
    assert real_node_subtests(tap) == ["react version is the pinned release"]


def test_file_placeholder_does_not_count() -> None:
    tap = (
        "# Subtest: /workspace/apps/web/test/react-pin.test.mjs\n"
        "ok 1 - /workspace/apps/web/test/react-pin.test.mjs\n"
        "# tests 1\n"
        "# pass 1\n"
    )
    assert real_node_subtests(tap) == []


def test_indented_test_counts_and_suite_name_does_not() -> None:
    tap = (
        "# Subtest: outer\n"
        "    # Subtest: inner real\n"
        "    ok 1 - inner real\n"
        "      ---\n"
        "      type: 'test'\n"
        "      ...\n"
        "ok 1 - outer\n"
        "  ---\n"
        "  type: 'suite'\n"
        "  ...\n"
        "# pass 1\n"
        "# fail 0\n"
    )
    assert real_node_subtests(tap) == ["inner real"]


def _node_file_status(path: Path) -> int:
    completed = subprocess.run(node_command(path), check=False, text=True, capture_output=True)
    return node_run_status(completed.stdout + completed.stderr, completed.returncode)


def test_describe_only_file_fails(tmp_path: Path) -> None:
    path = tmp_path / "only-describe.test.mjs"
    path.write_text(
        'import { describe } from "node:test";\ndescribe("suite only", () => {});\n',
        encoding="utf-8",
    )
    assert _node_file_status(path) != 0


def test_skip_only_file_fails(tmp_path: Path) -> None:
    path = tmp_path / "only-skip.test.mjs"
    path.write_text(
        'import { test } from "node:test";\ntest("skipped case", { skip: true }, () => {});\n',
        encoding="utf-8",
    )
    assert _node_file_status(path) != 0


def test_todo_only_file_fails(tmp_path: Path) -> None:
    path = tmp_path / "only-todo.test.mjs"
    path.write_text(
        'import { test } from "node:test";\ntest("todo case", { todo: true }, () => {});\n',
        encoding="utf-8",
    )
    assert _node_file_status(path) != 0


def test_js_ts_and_nested_files_are_discovered_and_run(tmp_path: Path) -> None:
    js = tmp_path / "plain.test.js"
    js.write_text(
        'const { test } = require("node:test");\n'
        'const assert = require("node:assert");\n'
        'test("js real", () => { assert.equal(1, 1); });\n',
        encoding="utf-8",
    )
    nested = tmp_path / "nested" / "deep"
    nested.mkdir(parents=True)
    ts = nested / "deep.test.ts"
    ts.write_text(
        'import { test } from "node:test";\n'
        'import assert from "node:assert/strict";\n'
        'test("ts real", () => { assert.equal(1, 1); });\n',
        encoding="utf-8",
    )
    found = node_test_files(tmp_path)
    assert found == [ts, js]
    for path in found:
        assert _node_file_status(path) == 0


def test_empty_file_has_zero_real_tests(tmp_path: Path) -> None:
    path = tmp_path / "empty.test.mjs"
    path.write_text("", encoding="utf-8")
    completed = subprocess.run(
        ["node", "--test", "--test-reporter", "tap", str(path)],
        check=False,
        text=True,
        capture_output=True,
    )
    assert completed.returncode == 0
    assert real_node_subtests(completed.stdout) == []
