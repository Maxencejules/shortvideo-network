# Governance notes

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled.

Three authorities stay separate. None of them owns user content.

| Authority | Decides | Does not decide |
| --- | --- | --- |
| Protocol governance | Validator admission, bounds, and upgrades, through the council | Repository contents, or a user's file |
| Community governance | Local membership and moderation | Chain consensus, or what this repository ships |
| Maintainers | What code enters the reference repository | Chain state, and a release signature |

The detail already recorded for protocol governance and publication is [governance-authorities.md](governance-authorities.md#three-authorities). Community moderation is the goal 23 service. It is not the council and it is not a maintainer.

Release signing, tags, and the name of a publisher stay an owner action. The candidate workflow cannot do them. See [signing-procedure.md](signing-procedure.md).

The five roles in [../ownership.md](../ownership.md) are functions: product/application, chain/security, media/provider agents, deployment/on-call, and trust/safety. No person is named. The decision log is [../decision-register.md](../decision-register.md).
