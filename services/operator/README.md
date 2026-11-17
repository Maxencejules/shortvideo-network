# Operator dashboard

`svn-operator` is the loopback operator dashboard. It has a public read-only listener and a separate admin listener. It does not load a consensus private key and it does not submit a council message.

This note is not a production runbook. Public launch is disabled. There is no token. DR-24-01 through DR-24-12 are open. The role map is in `docs/operations/operator-dashboard.md`.

## Run

The config has no consensus key. `dev_sessions` mints in-memory bearers for development. Leave it false outside a test.

```text
svn-operator config.json
```

`public_listen` and `admin_listen` must bind on loopback. Provider, indexer, discovery, scheduler, and governance URLs must be loopback too. The admin token file is read by this process and is not returned in a response.

`GET /v1/status` on the public listener is the readings. A class of `absent` means the source did not report a value. A class of `stale` is shown as stale. `GET /v1/council` is the read-only proposal document and the upgrade instructions.

Admin actions require a permission role and a confirm phrase. A viewer, a moderator, and a council session cannot drain a provider. A provider session cannot read the moderation queue. `POST /v1/governance` and `POST /v1/policy` return `GOVERNANCE_NOT_SUBMITTED`.
