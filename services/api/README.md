# services/api

Authenticated upload intents, private quarantine, and a processing-job outbox. The runnable process is `cmd/svn-api`. It listens on a loopback address.

`svn_api.serve` still raises. FastAPI 0.162.0 stays the pinned family and does not open a second listener. There is no public sign-up route. Public launch stays disabled.

Development sessions are the fixtures in `fixtures/development.json`. The tokens use the `dev-fixture-` prefix. They are not secrets and they are refused when `environment` is `production`. Production starts the account service. Passkeys are verified by `github.com/go-webauthn/webauthn`. A passkey is an application login, not a chain key. DR-13-01 stays open: the owner has not approved this session boundary as the account contract. Delegated host-signing custody and account recovery stay blocked on DEC-001.

Proposed limits, labeled `PROPOSED`, are 100000000 input bytes and 60 seconds. Those ceilings cannot be raised in configuration. Per-account allowances in the fixture file are not a DEC-005 decision.

Quarantine files use generated hex names under a mode `0700` directory. No route serves those bytes. `POST /publish` returns 403. An upload status cannot be `PUBLISHED`.

The integration points for later goals are in `docs/architecture/uploads.md`.

```bash
export PATH="$(pwd)/.toolchains/go/bin:$PATH"
go test -count=1 -timeout 8m ./services/api/...
```

The tests start PostgreSQL 18 from `/usr/lib/postgresql/18/bin` and run `ffprobe`. They do not skip when either is missing.
