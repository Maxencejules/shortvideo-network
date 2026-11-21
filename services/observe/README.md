# svn-observe

Loopback control plane for goal 25. It records correlation ids, health and readiness, redacted logs, admission decisions, and a cost worksheet.

Thresholds in this process are placeholders. They are not a DEC-005 allowance. Alerts are not a spend cap. There is no budget owner, no rate card, and no on-call assignment. Public launch is disabled. The process does not write the chain and it does not store watch events.

The token comes from `SVN_OBSERVE_TOKEN`. The listen address is `SVN_OBSERVE_LISTEN` and must be a loopback host. The default is `127.0.0.1:0`.
