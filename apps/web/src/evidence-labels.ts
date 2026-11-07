/**
 * Evidence label catalog for the API and UI (goal 09, decision request DR-09-24).
 *
 * The strings are proposed, not approved. JSON.stringify(EVIDENCE_LABELS,
 * null, 2) plus a newline equals agents/auditor/labels/evidence-labels.json,
 * the catalog the audit producer embeds, byte for byte;
 * apps/web/test/evidence-labels.test.ts checks that, pins every sentence,
 * and applies the shared rules of
 * agents/auditor/labels/testdata/label-rules.json. A wording change edits
 * this file, the JSON catalog, and the pinned lists of the Go and Node
 * tests in one change.
 *
 * The mode is shown explicitly as "Auditor attestation" or "Provider receipt".
 * Every text describes an attestation; none claims a storage proof. Mode keys
 * are the kind names of the chain's evidence.KindName; state keys are the
 * short state names of evidence.StateName.
 *
 * Nothing renders these strings yet. Goals 20, 21, and 24 build the UI.
 */
export const EVIDENCE_LABELS = {
  catalog: "svn-evidence-labels-proposed-v0",
  claim_limit: "ATTESTATION_NOT_STORAGE_PROOF",
  modes: {
    AUDITOR_ATTESTATION: {
      label: "Auditor attestation",
      text: "An admitted auditor signed this statement about one lease. It is an attestation. It is not proof of storage.",
    },
    PROVIDER_RECEIPT: {
      label: "Provider receipt",
      text: "A transfer receipt attributed to a provider. This beta does not check its signature and does not record it on chain. It is an attestation. It is not proof of storage or of future delivery.",
    },
  },
  states: {
    RECORDED: {
      label: "Recorded",
      text: "The auditor reported the lease's manifest digest. The chain compared two digests and fetched nothing. The manifest digest is public. Reporting it does not show that any byte was fetched. A recorded attestation describes one past observation. It does not show that the lease is held or the content is available now.",
    },
    REJECTED: {
      label: "Rejected",
      text: "The auditor reported a digest that differs from the lease's manifest digest. This can mean a failed or mismatched check. The chain did not check why. A rejected record does not expire. The chain cannot tell whether the provider or the auditor caused the difference.",
    },
    EXPIRED: {
      label: "Expired",
      text: "The attestation's expiry time has passed.",
    },
  },
  event_accepted:
    "EvidenceAccepted means the record was written to the ledger. It does not mean the check passed.",
  acceptance: {
    label: "Provider acceptance",
    text: "The provider signed the package digest, byte count and terms. Acceptance is a promise, not possession.",
  },
  limitations: [
    "Auditors can lie.",
    "Providers may fetch from another host.",
    "Node IDs do not prove independent replicas.",
    "A valid signature does not make an observation true.",
    "Accounting success is not physical capacity or operator independence.",
    "A sampled check covers only the sampled segments.",
    "This auditor is not shown to be independent of the provider.",
    "No evidence record renews, extends or revives a lease, in any state.",
    "No cryptographic storage proof exists in this beta. It is a separate decision gate.",
  ],
} as const;

/** An evidence kind name with a mode entry: AUDITOR_ATTESTATION or PROVIDER_RECEIPT. */
export type EvidenceModeKey = keyof typeof EVIDENCE_LABELS.modes;

/** A short reported state name: RECORDED, REJECTED, or EXPIRED. */
export type EvidenceStateKey = keyof typeof EVIDENCE_LABELS.states;
