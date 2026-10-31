"""Negative fixtures for CI policy. These documents are not workflow files."""

from __future__ import annotations

from scripts.workflow_policy import check_workflow_text

BASE = """
name: sample
on:
  pull_request:
permissions:
  contents: read
jobs:
  unit:
    runs-on: ubuntu-24.04
    timeout-minutes: 5
    steps:
      - run: echo ok
"""


def test_real_workflow_passes() -> None:
    from pathlib import Path

    from scripts.workflow_policy import check_workflow

    path = Path(__file__).resolve().parents[2] / ".github" / "workflows" / "ci.yml"
    assert check_workflow(path) == []


def test_continue_on_error_is_rejected() -> None:
    text = BASE.replace("timeout-minutes: 5\n", "timeout-minutes: 5\n    continue-on-error: true\n")
    errors = check_workflow_text("bad.yml", text)
    assert any("continue-on-error" in error for error in errors)


def test_step_continue_on_error_is_rejected() -> None:
    text = BASE.replace("- run: echo ok\n", "- run: echo ok\n        continue-on-error: true\n")
    errors = check_workflow_text("bad.yml", text)
    assert any("continue-on-error" in error for error in errors)


def test_event_title_in_run_is_rejected() -> None:
    text = BASE.replace(
        "- run: echo ok\n",
        '- run: echo "${{ github.event.pull_request.title }}"\n',
    )
    errors = check_workflow_text("bad.yml", text)
    assert any("interpolated" in error for error in errors)


def test_workflow_run_trigger_is_rejected() -> None:
    text = BASE.replace("pull_request:", "pull_request:\n  workflow_run:")
    errors = check_workflow_text("bad.yml", text)
    assert any("workflow_run" in error for error in errors)


def test_expression_continue_on_error_is_rejected() -> None:
    expr = "${{ " + "true }}"
    text = BASE.replace(
        "- run: echo ok\n", "- run: echo ok\n        continue-on-error: " + expr + "\n"
    )
    errors = check_workflow_text("bad.yml", text)
    assert errors
    assert all("continue-on-error" in error for error in errors)


def test_quoted_continue_on_error_is_rejected() -> None:
    text = BASE.replace(
        "- run: echo ok\n",
        '- run: echo ok\n        continue-on-error: "true"\n',
    )
    errors = check_workflow_text("bad.yml", text)
    assert errors
    assert all("continue-on-error" in error for error in errors)


def test_head_ref_in_run_is_rejected() -> None:
    text = BASE.replace("- run: echo ok\n", '- run: echo "${{ github.head_ref }}"\n')
    errors = check_workflow_text("bad.yml", text)
    assert errors
    assert all("interpolated" in error for error in errors)


def test_secrets_index_is_rejected() -> None:
    run = "echo ${{ " + "secrets" + '["X"] }}'
    text = BASE.replace("- run: echo ok\n", "- run: " + run + "\n")
    errors = check_workflow_text("bad.yml", text)
    assert errors
    assert all("secrets index" in error for error in errors)


def test_tojson_secrets_is_rejected() -> None:
    run = "echo ${{ " + "toJSON(" + "secrets" + ") }}"
    text = BASE.replace("- run: echo ok\n", "- run: " + run + "\n")
    errors = check_workflow_text("bad.yml", text)
    assert errors
    assert all("toJSON(secrets)" in error for error in errors)


def test_secrets_inherit_is_rejected() -> None:
    line = "    " + "secrets" + ": " + "inherit" + "\n"
    text = BASE.replace("    timeout-minutes: 5\n", "    timeout-minutes: 5\n" + line)
    assert check_workflow_text("bad.yml", BASE) == []
    errors = check_workflow_text("bad.yml", text)
    assert errors
    assert all("secrets inherit" in error for error in errors)
