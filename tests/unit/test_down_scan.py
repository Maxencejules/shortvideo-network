"""The liveness scan is an AST call graph, not a source regex."""

from __future__ import annotations

import importlib.util
from pathlib import Path
from types import ModuleType

ROOT = Path(__file__).resolve().parents[2]


def _checker() -> ModuleType:
    path = ROOT / "docs" / "evidence" / "goal-02" / "check_protocol.py"
    spec = importlib.util.spec_from_file_location("goal02_check_protocol", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_state_get_and_helper_are_reads() -> None:
    checker = _checker()
    direct = 'def op_audit(state, step):\n    return state.get("down")\n'
    assert checker.handler_reads_down(direct, entry="op_audit") is True
    helper = (
        "def helper(state):\n"
        '    return state.get("down")\n'
        "def op_audit(state, step):\n"
        "    return helper(state)\n"
    )
    assert checker.handler_reads_down(helper, entry="op_audit") is True


def test_comment_and_string_are_not_reads() -> None:
    checker = _checker()
    comment = 'def op_audit(state, step):\n    # state["down"]\n    return None\n'
    literal = "def op_audit(state, step):\n    note = 'state[\"down\"]'\n    return note\n"
    assert checker.handler_reads_down(comment, entry="op_audit") is False
    assert checker.handler_reads_down(literal, entry="op_audit") is False


def test_live_handlers_do_not_read_down() -> None:
    checker = _checker()
    assert checker.handler_down_errors() == []
    assert checker.pin_self_test() == []
