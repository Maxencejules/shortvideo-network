# Operator dashboard and node onboarding

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. No person is assigned to deployment, trust and safety, or chain security. This page is not an admission decision and not evidence of an independent operator.

The dashboard is `svn-operator`. It listens on two loopback ports. The public port is read-only. The admin port takes development sessions and local actions. Neither port accepts a consensus private key, and neither port submits a council message.

## Role and permission map

A development session has one permission role. An onboarding record has any subset of the four duties. Selecting storage and delivery does not select validator and does not grant the council role.

| Permission role | May read | May do | Refused |
| --- | --- | --- | --- |
| viewer | Public status, council text, onboarding record | Nothing on the admin port | Drain, limits, handoff, disable, renewal, moderation queue, audit |
| provider | The same public reads, and the provider rows of the audit | Onboarding, local quota and bandwidth, stop-new-offers, drain, handoff, renewal review, local endpoint disable | Moderation queue, governance submit, policy submit |
| moderator | The local moderation queue index | Read that index | Drain, limits, handoff, onboarding, audit, governance submit |
| council | Public council text and the governance audit rows | Nothing that signs | Governance submit, policy submit, drain, queue narratives |

The queue index drops complaint text, complainant ids, and appeal notes. Case decisions stay on the discovery process.

Duties, chosen independently:

| Duty | What onboarding records | What it does not do |
| --- | --- | --- |
| storage and delivery | The choice, and the provider status when a provider URL is configured | It does not admit the provider on the chain |
| processing | The choice, and a file count when a queue directory is configured | It does not start an encoder |
| account and discovery | The choice. The discovery URL is pending when it is unset | It does not create an account |
| validator | The choice, marked pending, with the upgrade instructions | It does not load a consensus key and it does not start a validator |

`storage_forces_validator` is false. `independence_proven` is false. `single_machine_demo` is true. A failure-domain string is a declaration. Several node ids on this machine do not change those flags.

## Readings

Each reading has a class: configured, measured, pending, stale, or absent. Absent means the source did not report the value. The dashboard does not replace that with zero. A stale indexer or an uncertain chain height stays marked stale. Local disk bytes are a statfs observation of this process, not a minimum for every machine. Bandwidth shown from the provider is the configured limit. Bytes fetched and bytes served are counts the provider process recorded. The scheduler does not expose a job queue, so that reading stays absent.

## Local actions

Limits, stop-new-offers, drain, handoff, renewal review, and endpoint disable require the provider role and a confirm phrase. Drain refuses new offers and does not delete bytes. Handoff withdraws a serving lease only after the configured successor reports the same content, and the provider response keeps the chain end flag false. Renewal review is stored locally and is not broadcast. Changing declared capacity, posting governance, and posting policy return `GOVERNANCE_NOT_SUBMITTED` and do not call a chain.

## Upgrade instructions

Read Query/Policy from the node. Compare the release digest with the reviewed manifest. Install the reviewed binary beside the running one. Do not copy a validator key into this dashboard. The steps for the binary itself are in [operator-installation.md](operator-installation.md).

## Tasks that still need an administrator

- Name who may operate validators, storage, processing, and account services (DEC-002). This dashboard does not.
- Bind dashboard sessions to account-service identities. Development sessions are in memory.
- Point the governance URL at a read-only chain query. Until then proposals are absent rather than invented.
- Approve a renewal path. This dashboard does not submit one.
- Decide local quotas. A limit changed here is process configuration, not a chain allowance.
- Install a processing worker, an account service, or a validator. Onboarding records the choice only.
- Run the moderator queue decisions on the discovery process. This dashboard does not decide a case.
- Walk this dashboard with a second person on another machine. That walkthrough is not done.
