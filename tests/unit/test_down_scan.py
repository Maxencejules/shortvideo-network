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


def test_each_static_form_is_a_read() -> None:
    checker = _checker()
    forms = {
        "const key": (
            'DOWN_FIELD = "down"\n\ndef op_audit(state, step):\n    return state[DOWN_FIELD]\n'
        ),
        "setdefault": ('def op_audit(state, step):\n    return state.setdefault("down", [])\n'),
        "dict get": ('def op_audit(state, step):\n    return dict.get(state, "down")\n'),
        "items": (
            "def op_audit(state, step):\n    for key, value in state.items():\n        return key\n"
        ),
        "module alias": (
            "def helper(state):\n"
            '    return state.get("down")\n'
            "_real = helper\n"
            "_alias = _real\n"
            "def op_audit(state, step):\n"
            "    return _alias(state)\n"
        ),
        "f-string": 'def op_audit(state, step):\n    return state[f"down"]\n',
        "f-string expr": "def op_audit(state, step):\n    return state[f\"{'down'}\"]\n",
        "membership": 'def op_audit(state, step):\n    return "down" in state\n',
    }
    for label, source in forms.items():
        assert checker.handler_reads_down(source, entry="op_audit") is True, label


def test_nearby_forms_are_not_reads() -> None:
    checker = _checker()
    clean = [
        'def op_audit(state, step):\n    return state["offers"].items()\n',
        'def op_audit(state, step):\n    return "up" in state\n',
        'def op_audit(state, step):\n    return dict.get(state, "validators")\n',
        'def op_audit(state, step):\n    return state.setdefault("capacity", 1)\n',
    ]
    for source in clean:
        assert checker.handler_reads_down(source, entry="op_audit") is False


def test_dynamic_forms_are_reads() -> None:
    checker = _checker()
    forms = {
        "getattr": 'def op_audit(state, step):\n    return getattr(state, "down")\n',
        "itemgetter": "def op_audit(state, step):\n    return itemgetter('down')(state)\n",
        "join": 'def op_audit(state, step):\n    return state["".join(["down"])]\n',
        "values": "def op_audit(state, step):\n    return state.values()\n",
        "star": "def op_audit(state, step):\n    return {**state}\n",
        "handlers get": (
            "def reads(state, step):\n"
            '    return state["down"]\n'
            'TX_HANDLERS = {"Bad": reads}\n'
            "def op_audit(state, step):\n"
            '    return TX_HANDLERS.get(step["op"])(state, step)\n'
        ),
    }
    for label, source in forms.items():
        assert checker.handler_reads_down(source, entry="op_audit") is True, label


def test_constant_handler_get_does_not_follow_other_entries() -> None:
    checker = _checker()
    source = (
        "def reads(state, step):\n"
        '    return state["down"]\n'
        "def clean(state, step):\n"
        "    return None\n"
        'TX_HANDLERS = {"Bad": reads, "Ok": clean}\n'
        "def op_audit(state, step):\n"
        '    return TX_HANDLERS.get("Ok")(state, step)\n'
    )
    assert checker.handler_reads_down(source, entry="op_audit") is False


def test_live_handlers_do_not_read_down() -> None:
    checker = _checker()
    assert checker.handler_down_errors() == []
    assert checker.pin_self_test() == []
