# Moderation, blocking and removal

This note is the development description of the local safety workflow. It is not legal approval. Public launch is disabled. DEC-003 does not name rule text or an appeal authority. DEC-004 does not set a retention period. DEC-007 does not set an audience or an age rule.

## What a removal does

A removal delists the item on services this operator runs. It asks participating hosts to delete the copy they hold and to stop a lease locally. It drops cache entries those services issued. It does not erase an independent copy. It does not rewrite a historical ledger record. A committed transaction does not force a service to keep distributing the item.

## Local delist

Local delist runs before any new chain height. A stalled chain does not delay it. Repair and a later publication read refuse the delisted id. An unreachable host is recorded on its own and is not a successful deletion. The five-minute figure in the blueprint is a PROPOSED gate for controlled services, not a promise.

## Records kept

Until an owner sets a retention period, the process keeps the minimum: the decision outcome, the reason code, the admin audit row, and the delist acknowledgements. The complaint narrative and an appeal statement are dropped when that step is decided. They are visible only to a moderator while the step is open. They are not written to the chain.

## Placeholders

Contact roles are `PLACEHOLDER unassigned`. A contact file may replace those strings. It cannot set legal approval or enable public launch. Validators do not decide whether a video is lawful.
