# Developer entry points. devnet, smoke, fault, and release targets exist
# and fail closed until the goals that implement them.

ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
PY := $(ROOT)/.venv/bin/python
RUFF := $(ROOT)/.venv/bin/ruff
MYPY := $(ROOT)/.venv/bin/mypy

ifeq ($(wildcard $(PY)),)
PY := python3
endif

.PHONY: install fmt lint typecheck proto proto-check test unit-python unit-node unit-go contract sbom secrets policy build devnet-up devnet-down smoke fault release-check check env-diagnose env-install env-start env-stop env-reset env-smoke env-devnet-keys env-compose

install:
	python3 -m venv $(ROOT)/.venv
	$(ROOT)/.venv/bin/pip install --require-hashes -r $(ROOT)/requirements.lock
	npm ci

fmt:
	$(RUFF) format --check services workers/media packages/protocol tests scripts
	bash scripts/gofmt_check.sh

lint:
	$(RUFF) check services workers/media packages/protocol tests scripts
	$(PY) scripts/workflow_policy.py
	$(PY) scripts/scan_secrets.py

typecheck:
	$(MYPY)
	npm run typecheck

proto:
	bash scripts/gen_proto.sh

proto-check: proto
	@untracked="$$(git ls-files --others --exclude-standard -- proto/svn proto/gen/python)"; \
	if [ -n "$$untracked" ]; then echo "$$untracked"; exit 1; fi
	git diff --exit-code -- proto/svn proto/gen/python

unit-python:
	$(PY) scripts/run_tests.py --only python

unit-node:
	$(PY) scripts/run_tests.py --only node

unit-go:
	$(PY) scripts/run_tests.py --only go

test: unit-python unit-node unit-go

contract:
	$(PY) docs/evidence/goal-01/check_requirements.py
	$(PY) docs/evidence/goal-02/check_protocol.py

sbom:
	$(PY) scripts/write_sbom.py --check

secrets:
	$(PY) scripts/scan_secrets.py

policy:
	$(PY) scripts/workflow_policy.py

build:
	@echo "build compiles scaffold packages. It does not produce a chain binary."
	go build -C chain ./app/
	go build -C agents/provider .
	npm run typecheck
	$(PY) -m compileall -q services workers/media packages/protocol scripts tests/unit

devnet-up devnet-down:
	@echo "$@ is not implemented. Goal 05 starts four validator processes." >&2
	@exit 2

smoke:
	@echo "smoke is not implemented. Goal 18 is the joined upload-to-playback path." >&2
	@exit 2

fault:
	@echo "fault is not implemented. Goal 29 owns fault injection." >&2
	@exit 2

release-check:
	@echo "release-check is not implemented. Goal 27 prepares a release. Public launch is disabled." >&2
	@exit 2

env-diagnose:
	bash scripts/env/diagnose.sh

env-install:
	bash scripts/env/install.sh

env-start:
	bash scripts/env/start.sh

env-stop:
	bash scripts/env/stop.sh

env-reset:
	bash scripts/env/reset.sh --workspace "$(WORKSPACE)" --confirm "$(CONFIRM)"

env-smoke:
	bash scripts/env/smoke.sh

env-devnet-keys:
	bash scripts/env/devnet-keys.sh --workspace "$(or $(WORKSPACE),dev)"

env-compose:
	bash scripts/env/compose.sh

check: fmt lint typecheck proto-check sbom test contract build
	@echo "unimplemented targets stay failed on purpose"
	@$(MAKE) devnet-up; status=$$?; test $$status -ne 0
