# Cost worksheet

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. This sheet separates measured units from rates. It does not forecast a monthly bill. CAD is a proposed planning preference and is not an approved currency. No rate, tax, or overage figure is filled in here.

## Columns

| Column | Who fills it | What it is |
| --- | --- | --- |
| Measured delivered bytes, by class | `svn-observe`, from readings | user payload, replication, repair, prefetch, and backup, each kept apart |
| Measured storage bytes, by class | `svn-observe`, from readings | the same classes, as stored bytes, not added into delivered bytes |
| Operator rate | the operator | minor units per byte, blank until supplied |
| Currency | the operator | blank until supplied. The proposed preference is CAD |
| Period product | the process, only when every rate and the currency were supplied | measured units times the supplied rates for the recorded readings. The label is not a monthly forecast |
| Monthly bill | nobody, until a real rate card and a real period exist | this sheet leaves it empty |
| Founder cash | the operator | a supplied amount, not added to network bytes and not a rate |
| Volunteer contribution | the operator | a note, not a cash figure invented from the note |

Network bytes are the sum of the delivered classes. Storage is a separate column. Founder cash and volunteer notes are separate columns. Adding those columns together is not a bill.

## Unmeasured

Protocol overhead, relay traffic, record-layer bytes, chain gossip, interface bandwidth, and the scheduler job queue are not in the measured columns. Do not enter a zero for them and do not treat the gap as a saving.

## What this sheet will not do

It will not invent a rate, a currency conversion, a tax line, or a monthly total. It will not describe an alert as a spend cap. It will not treat a placeholder admission threshold as an allowance. The runbooks are in [observability.md](observability.md).

The budget owner is unassigned. A product of units and rates is still not a forecast of a future month.
