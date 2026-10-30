#!/usr/bin/env python3
"""Check the Goal 02 protocol contracts and fixtures.

Run from the repository root:

    python3 docs/evidence/goal-02/check_protocol.py

Exit 0 means the assertions in this file held for this worktree.
A pass is a schema and fixture check. It is not a devnet, a security
audit, or an approval of DEC-001 through DEC-008.
"""

from __future__ import annotations

import hashlib
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(Path(__file__).resolve().parent))

import cases  # noqa: E402
import jcs  # noqa: E402

PROTOCOL = ROOT / "docs" / "protocol.md"
BOUNDS_PATH = ROOT / "docs" / "protocol" / "bounds.json"
PROTO_ROOT = ROOT / "docs" / "protocol" / "proto"
VECTORS = ROOT / "docs" / "protocol" / "fixtures" / "vectors.json"
DECISIONS = ROOT / "docs" / "decision-register.md"
PROGRESS = ROOT / "docs" / "progress.md"
PROGRESS_TSV = ROOT / "docs" / "progress.tsv"
PAYLOAD_DIGEST = "be9df594c416ecbaf50512e298a73fecf9f7a56e7ac83a0cf4edf87a6b522c48"
PAYLOAD_ZERO_DIGEST = "7eeb7c6d8fadfb2b28fea5d2cd42f1a85eaf2b773e79b24399d350b9b9be675a"
PINNED_FORBIDDEN_PATTERNS = (
    "mint",
    "reward",
    "slash",
    "token",
    "balance",
    "stake",
    "ip",
    "video_data",
    "video_payload",
    "video_bytes",
    "media_bytes",
    "email",
    "username",
    "watch_event",
    "complaint",
    "mnemonic",
    "private_key",
    "secret",
    "password",
    "phone",
    "uploader_ip",
    "storage_proof",
    "addr",
    "device",
    "geo",
    "viewer",
    "payout",
    "bond",
    "fee",
    "coin",
    "denom",
    "inflation",
    "issuance",
    "media",
    "chunk",
    "blob",
)
PINNED_NAME_EXEMPTIONS = (
    "mint_module_enabled",
    "distribution_module_enabled",
    "slashing_module_enabled",
    "staking_module_enabled",
    "permissionless_validator_creation",
    "PRIVACY_CLASS_SECRET",
    "mintModuleEnabled",
    "slashingModuleEnabled",
    "ERROR_CODE_FEE_REJECTED",
    "SIGNATURE_DOMAIN_MEDIA_MANIFEST",
)
PINNED_ONEOF_MEMBERS = (
    "validator_power",
    "governed_limit",
    "admit_provider",
    "disable_provider",
    "force_withdraw",
)
PINNED_RPCS = (
    "RegisterProvider",
    "UpdateProvider",
    "RotateProviderKey",
    "DrainProvider",
    "AdmitProvider",
    "DisableProvider",
    "RegisterContent",
    "WithdrawContent",
    "ForceWithdraw",
    "OfferLease",
    "AcceptLease",
    "RenewLease",
    "EndLease",
    "SubmitAuditBatch",
    "ProposeChange",
    "ApproveChange",
    "ExecuteChange",
)
PINNED_GOVERNED_CAPS = {
    "max_evidence_batch": 32,
    "max_page_limit": 100,
    "max_open_offers": 1024,
    "max_expiry_steps": 128,
    "max_package_bytes": 268435456,
}
DESCRIPTOR_NAME_RE = re.compile(r'(?:^|[^\w])name: "([^"]*)"')
NAME_KINDS = {"message", "field", "enum", "enum_value", "oneof", "rpc", "json_name", "service"}
# Filled after the descriptor parser can list the real protocol. Empty fails the allowlist check.
PINNED_DESCRIPTOR_NAMES: tuple[str, ...] = (
    'enum:ContentState',
    'enum:DigestAlgorithm',
    'enum:ErrorCode',
    'enum:EvidenceKind',
    'enum:EvidenceState',
    'enum:LeaseState',
    'enum:OperationStatus',
    'enum:PrivacyClass',
    'enum:ProposalState',
    'enum:ProviderState',
    'enum:PublicationStatus',
    'enum:QuorumLossBehavior',
    'enum:Reachability',
    'enum:SignatureDomain',
    'enum:SubmitterRole',
    'enum_value:CONTENT_STATE_PENDING',
    'enum_value:CONTENT_STATE_UNSPECIFIED',
    'enum_value:CONTENT_STATE_WITHDRAWN',
    'enum_value:DIGEST_ALGORITHM_SHA256',
    'enum_value:DIGEST_ALGORITHM_UNSPECIFIED',
    'enum_value:ERROR_CODE_BOUNDS',
    'enum_value:ERROR_CODE_CAPACITY',
    'enum_value:ERROR_CODE_CONFLICT',
    'enum_value:ERROR_CODE_DECISION_BLOCKED',
    'enum_value:ERROR_CODE_DUPLICATE_ID',
    'enum_value:ERROR_CODE_EXPIRED',
    'enum_value:ERROR_CODE_FEE_REJECTED',
    'enum_value:ERROR_CODE_FORBIDDEN',
    'enum_value:ERROR_CODE_INTEGRITY',
    'enum_value:ERROR_CODE_MALFORMED',
    'enum_value:ERROR_CODE_NOT_FOUND',
    'enum_value:ERROR_CODE_OUT_OF_RANGE',
    'enum_value:ERROR_CODE_OVERFLOW',
    'enum_value:ERROR_CODE_QUORUM',
    'enum_value:ERROR_CODE_REPLAY',
    'enum_value:ERROR_CODE_STALE_VERSION',
    'enum_value:ERROR_CODE_UNAUTHORIZED',
    'enum_value:ERROR_CODE_UNDERFLOW',
    'enum_value:ERROR_CODE_UNKNOWN_FIELD',
    'enum_value:ERROR_CODE_UNSPECIFIED',
    'enum_value:ERROR_CODE_WITHDRAWN',
    'enum_value:ERROR_CODE_WRONG_DOMAIN',
    'enum_value:EVIDENCE_KIND_AUDITOR_ATTESTATION',
    'enum_value:EVIDENCE_KIND_PROVIDER_RECEIPT',
    'enum_value:EVIDENCE_KIND_UNSPECIFIED',
    'enum_value:EVIDENCE_STATE_EXPIRED',
    'enum_value:EVIDENCE_STATE_RECORDED',
    'enum_value:EVIDENCE_STATE_REJECTED',
    'enum_value:EVIDENCE_STATE_UNSPECIFIED',
    'enum_value:LEASE_STATE_ACTIVE',
    'enum_value:LEASE_STATE_ENDED',
    'enum_value:LEASE_STATE_EXPIRED',
    'enum_value:LEASE_STATE_OFFERED',
    'enum_value:LEASE_STATE_RENEWED',
    'enum_value:LEASE_STATE_UNSPECIFIED',
    'enum_value:OPERATION_STATUS_COMMITTED',
    'enum_value:OPERATION_STATUS_FAILED',
    'enum_value:OPERATION_STATUS_PENDING_STALL',
    'enum_value:OPERATION_STATUS_REQUESTED',
    'enum_value:OPERATION_STATUS_SUBMITTED',
    'enum_value:OPERATION_STATUS_UNSPECIFIED',
    'enum_value:PRIVACY_CLASS_PRIVATE',
    'enum_value:PRIVACY_CLASS_PUBLIC_COORDINATION',
    'enum_value:PRIVACY_CLASS_PUBLIC_RECORD',
    'enum_value:PRIVACY_CLASS_RESTRICTED_CASE',
    'enum_value:PRIVACY_CLASS_SECRET',
    'enum_value:PRIVACY_CLASS_UNSPECIFIED',
    'enum_value:PROPOSAL_STATE_EXECUTED',
    'enum_value:PROPOSAL_STATE_EXPIRED',
    'enum_value:PROPOSAL_STATE_OPEN',
    'enum_value:PROPOSAL_STATE_QUEUED',
    'enum_value:PROPOSAL_STATE_REJECTED',
    'enum_value:PROPOSAL_STATE_UNSPECIFIED',
    'enum_value:PROVIDER_STATE_ADMITTED',
    'enum_value:PROVIDER_STATE_APPLIED',
    'enum_value:PROVIDER_STATE_DISABLED',
    'enum_value:PROVIDER_STATE_DRAINING',
    'enum_value:PROVIDER_STATE_UNSPECIFIED',
    'enum_value:PUBLICATION_STATUS_APPROVED',
    'enum_value:PUBLICATION_STATUS_DELISTED',
    'enum_value:PUBLICATION_STATUS_NOT_REQUESTED',
    'enum_value:PUBLICATION_STATUS_REJECTED',
    'enum_value:PUBLICATION_STATUS_REVIEW_PENDING',
    'enum_value:PUBLICATION_STATUS_UNSPECIFIED',
    'enum_value:QUORUM_LOSS_BEHAVIOR_UNSET',
    'enum_value:QUORUM_LOSS_BEHAVIOR_UNSPECIFIED',
    'enum_value:REACHABILITY_DEGRADED',
    'enum_value:REACHABILITY_REACHABLE',
    'enum_value:REACHABILITY_UNKNOWN',
    'enum_value:REACHABILITY_UNREACHABLE',
    'enum_value:REACHABILITY_UNSPECIFIED',
    'enum_value:SIGNATURE_DOMAIN_CAPABILITY',
    'enum_value:SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION',
    'enum_value:SIGNATURE_DOMAIN_HOSTING_INTENT',
    'enum_value:SIGNATURE_DOMAIN_INVENTORY',
    'enum_value:SIGNATURE_DOMAIN_MEDIA_MANIFEST',
    'enum_value:SIGNATURE_DOMAIN_PROVIDER_ACCEPT',
    'enum_value:SIGNATURE_DOMAIN_PROVIDER_DRAIN',
    'enum_value:SIGNATURE_DOMAIN_PROVIDER_END',
    'enum_value:SIGNATURE_DOMAIN_PROVIDER_RENEW',
    'enum_value:SIGNATURE_DOMAIN_PROVIDER_ROTATE',
    'enum_value:SIGNATURE_DOMAIN_PUBLIC_RECORD',
    'enum_value:SIGNATURE_DOMAIN_TRANSFER_RECEIPT',
    'enum_value:SIGNATURE_DOMAIN_UNSPECIFIED',
    'enum_value:SUBMITTER_ROLE_ADMITTED_PROVIDER',
    'enum_value:SUBMITTER_ROLE_ADMITTED_SERVICE',
    'enum_value:SUBMITTER_ROLE_AUDITOR',
    'enum_value:SUBMITTER_ROLE_AUTHORITY',
    'enum_value:SUBMITTER_ROLE_BROADCASTER',
    'enum_value:SUBMITTER_ROLE_UNSPECIFIED',
    'field:account',
    'field:activation',
    'field:address',
    'field:admit_provider',
    'field:admitted_capacity_bytes',
    'field:algorithm',
    'field:allow_devnet_test_parameters',
    'field:approved',
    'field:approved_council',
    'field:approved_policy',
    'field:approver_keys',
    'field:attestation',
    'field:auditor_signature',
    'field:authority',
    'field:authority_end',
    'field:base_policy_version',
    'field:batch_id',
    'field:blocked_decisions',
    'field:business_id',
    'field:byte_count',
    'field:canonical_profile_version',
    'field:chain_id',
    'field:committed_unix_seconds',
    'field:consensus_pubkey',
    'field:content',
    'field:content_id',
    'field:content_version',
    'field:contents',
    'field:current_epoch',
    'field:current_key_signature',
    'field:deadline_unix_seconds',
    'field:decision_id',
    'field:declared_capacity_bytes',
    'field:delay_seconds',
    'field:devnet_test_parameters',
    'field:devnet_test_seats',
    'field:digest',
    'field:disable_provider',
    'field:distribution_module_enabled',
    'field:domain',
    'field:endpoint_manifest_digest',
    'field:epoch',
    'field:evidence',
    'field:evidence_id',
    'field:execute_after_unix_seconds',
    'field:expected_generation',
    'field:expiry_unix_seconds',
    'field:force_withdraw',
    'field:generation',
    'field:genesis_unix_seconds',
    'field:governed_limit',
    'field:height',
    'field:history_erased',
    'field:hosting_intent',
    'field:idempotent',
    'field:items',
    'field:key_generation',
    'field:key_id',
    'field:kind',
    'field:label',
    'field:lease_id',
    'field:leases',
    'field:limit',
    'field:limit_key',
    'field:manifest_digest',
    'field:manifest_sha256',
    'field:message_type',
    'field:mint_module_enabled',
    'field:new_signing_key_id',
    'field:new_signing_pubkey',
    'field:next_page_key',
    'field:node_id',
    'field:not_after_unix_seconds',
    'field:numeric_value',
    'field:object_id',
    'field:observation_digest',
    'field:offer_id',
    'field:offers',
    'field:operator_group_id',
    'field:package_bytes',
    'field:page',
    'field:page_key',
    'field:parameter_key',
    'field:payload',
    'field:payload_digest',
    'field:pending_validator_power',
    'field:permissionless_validator_creation',
    'field:policy',
    'field:policy_version',
    'field:power',
    'field:previous_signing_keys',
    'field:proposal_id',
    'field:proposals',
    'field:protocol_version',
    'field:provider',
    'field:provider_acceptance',
    'field:provider_id',
    'field:provider_signature',
    'field:providers',
    'field:public_key',
    'field:quorum_loss',
    'field:reason_digest',
    'field:records',
    'field:reject',
    'field:released_bytes',
    'field:reservation_delta',
    'field:reservation_held',
    'field:reserved_bytes',
    'field:result',
    'field:retention_decision_id',
    'field:retention_value_version',
    'field:retired_unix_seconds',
    'field:role',
    'field:role_name',
    'field:safety_caps',
    'field:safety_max_evidence_batch',
    'field:safety_max_expiry_steps_per_block',
    'field:safety_max_gas_per_tx',
    'field:safety_max_open_offers_per_provider',
    'field:safety_max_package_bytes',
    'field:safety_max_page_limit',
    'field:safety_max_tx_bytes',
    'field:scheduler',
    'field:schema_version',
    'field:seat_id',
    'field:signature',
    'field:signer',
    'field:signer_pubkey',
    'field:signing_key_id',
    'field:signing_pubkey',
    'field:slashing_module_enabled',
    'field:staking_module_enabled',
    'field:start_unix_seconds',
    'field:state',
    'field:submitters',
    'field:submitting_host',
    'field:term_version',
    'field:terms_digest',
    'field:tombstone',
    'field:tombstones',
    'field:tx_hash',
    'field:unix_seconds',
    'field:validator_power',
    'field:validators',
    'field:value',
    'field:value_version',
    'json_name:account',
    'json_name:activation',
    'json_name:address',
    'json_name:admitProvider',
    'json_name:admittedCapacityBytes',
    'json_name:algorithm',
    'json_name:allowDevnetTestParameters',
    'json_name:approved',
    'json_name:approvedCouncil',
    'json_name:approvedPolicy',
    'json_name:approverKeys',
    'json_name:attestation',
    'json_name:auditorSignature',
    'json_name:authority',
    'json_name:authorityEnd',
    'json_name:basePolicyVersion',
    'json_name:batchId',
    'json_name:blockedDecisions',
    'json_name:businessId',
    'json_name:byteCount',
    'json_name:canonicalProfileVersion',
    'json_name:chainId',
    'json_name:committedUnixSeconds',
    'json_name:consensusPubkey',
    'json_name:content',
    'json_name:contentId',
    'json_name:contentVersion',
    'json_name:contents',
    'json_name:currentEpoch',
    'json_name:currentKeySignature',
    'json_name:deadlineUnixSeconds',
    'json_name:decisionId',
    'json_name:declaredCapacityBytes',
    'json_name:delaySeconds',
    'json_name:devnetTestParameters',
    'json_name:devnetTestSeats',
    'json_name:digest',
    'json_name:disableProvider',
    'json_name:distributionModuleEnabled',
    'json_name:domain',
    'json_name:endpointManifestDigest',
    'json_name:epoch',
    'json_name:evidence',
    'json_name:evidenceId',
    'json_name:executeAfterUnixSeconds',
    'json_name:expectedGeneration',
    'json_name:expiryUnixSeconds',
    'json_name:forceWithdraw',
    'json_name:generation',
    'json_name:genesisUnixSeconds',
    'json_name:governedLimit',
    'json_name:height',
    'json_name:historyErased',
    'json_name:hostingIntent',
    'json_name:idempotent',
    'json_name:items',
    'json_name:keyGeneration',
    'json_name:keyId',
    'json_name:kind',
    'json_name:label',
    'json_name:leaseId',
    'json_name:leases',
    'json_name:limit',
    'json_name:limitKey',
    'json_name:manifestDigest',
    'json_name:manifestSha256',
    'json_name:messageType',
    'json_name:mintModuleEnabled',
    'json_name:newSigningKeyId',
    'json_name:newSigningPubkey',
    'json_name:nextPageKey',
    'json_name:nodeId',
    'json_name:notAfterUnixSeconds',
    'json_name:numericValue',
    'json_name:objectId',
    'json_name:observationDigest',
    'json_name:offerId',
    'json_name:offers',
    'json_name:operatorGroupId',
    'json_name:packageBytes',
    'json_name:page',
    'json_name:pageKey',
    'json_name:parameterKey',
    'json_name:payload',
    'json_name:payloadDigest',
    'json_name:pendingValidatorPower',
    'json_name:permissionlessValidatorCreation',
    'json_name:policy',
    'json_name:policyVersion',
    'json_name:power',
    'json_name:previousSigningKeys',
    'json_name:proposalId',
    'json_name:proposals',
    'json_name:protocolVersion',
    'json_name:provider',
    'json_name:providerAcceptance',
    'json_name:providerId',
    'json_name:providerSignature',
    'json_name:providers',
    'json_name:publicKey',
    'json_name:quorumLoss',
    'json_name:reasonDigest',
    'json_name:records',
    'json_name:reject',
    'json_name:releasedBytes',
    'json_name:reservationDelta',
    'json_name:reservationHeld',
    'json_name:reservedBytes',
    'json_name:result',
    'json_name:retentionDecisionId',
    'json_name:retentionValueVersion',
    'json_name:retiredUnixSeconds',
    'json_name:role',
    'json_name:roleName',
    'json_name:safetyCaps',
    'json_name:safetyMaxEvidenceBatch',
    'json_name:safetyMaxExpiryStepsPerBlock',
    'json_name:safetyMaxGasPerTx',
    'json_name:safetyMaxOpenOffersPerProvider',
    'json_name:safetyMaxPackageBytes',
    'json_name:safetyMaxPageLimit',
    'json_name:safetyMaxTxBytes',
    'json_name:scheduler',
    'json_name:schemaVersion',
    'json_name:seatId',
    'json_name:signature',
    'json_name:signer',
    'json_name:signerPubkey',
    'json_name:signingKeyId',
    'json_name:signingPubkey',
    'json_name:slashingModuleEnabled',
    'json_name:stakingModuleEnabled',
    'json_name:startUnixSeconds',
    'json_name:state',
    'json_name:submitters',
    'json_name:submittingHost',
    'json_name:termVersion',
    'json_name:termsDigest',
    'json_name:tombstone',
    'json_name:tombstones',
    'json_name:txHash',
    'json_name:unixSeconds',
    'json_name:validatorPower',
    'json_name:validators',
    'json_name:value',
    'json_name:valueVersion',
    'message:ActivationPoint',
    'message:AdmitProviderChange',
    'message:AppGenesis',
    'message:AuthorityState',
    'message:BlockedDecisionParameter',
    'message:Content',
    'message:ContentRegistered',
    'message:ContentWithdrawn',
    'message:DevnetTestParameter',
    'message:DevnetTestSeat',
    'message:Digest',
    'message:DisableProviderChange',
    'message:EvidenceAccepted',
    'message:EvidenceItem',
    'message:EvidenceRecord',
    'message:ForceWithdrawChange',
    'message:GenesisState',
    'message:GovernedLimitChange',
    'message:InnerSignature',
    'message:Lease',
    'message:LeaseAccepted',
    'message:LeaseEnded',
    'message:LeaseOffered',
    'message:LeaseRenewed',
    'message:MsgAcceptLease',
    'message:MsgAcceptLeaseResponse',
    'message:MsgAdmitProvider',
    'message:MsgAdmitProviderResponse',
    'message:MsgApproveChange',
    'message:MsgApproveChangeResponse',
    'message:MsgDisableProvider',
    'message:MsgDisableProviderResponse',
    'message:MsgDrainProvider',
    'message:MsgDrainProviderResponse',
    'message:MsgEndLease',
    'message:MsgEndLeaseResponse',
    'message:MsgExecuteChange',
    'message:MsgExecuteChangeResponse',
    'message:MsgForceWithdraw',
    'message:MsgForceWithdrawResponse',
    'message:MsgOfferLease',
    'message:MsgOfferLeaseResponse',
    'message:MsgProposeChange',
    'message:MsgProposeChangeResponse',
    'message:MsgRegisterContent',
    'message:MsgRegisterContentResponse',
    'message:MsgRegisterProvider',
    'message:MsgRegisterProviderResponse',
    'message:MsgRenewLease',
    'message:MsgRenewLeaseResponse',
    'message:MsgRotateProviderKey',
    'message:MsgRotateProviderKeyResponse',
    'message:MsgSubmitAuditBatch',
    'message:MsgSubmitAuditBatchResponse',
    'message:MsgUpdateProvider',
    'message:MsgUpdateProviderResponse',
    'message:MsgWithdrawContent',
    'message:MsgWithdrawContentResponse',
    'message:Offer',
    'message:PageRequest',
    'message:PageResponse',
    'message:PendingValidatorPower',
    'message:PolicyChanged',
    'message:PolicyState',
    'message:PreviousSigningKey',
    'message:Proposal',
    'message:ProposalExecuted',
    'message:ProposalPayload',
    'message:Provider',
    'message:ProviderRegistered',
    'message:ProviderStateChanged',
    'message:QueryActiveLeasesRequest',
    'message:QueryActiveLeasesResponse',
    'message:QueryContentRequest',
    'message:QueryContentResponse',
    'message:QueryDeadlinesRequest',
    'message:QueryDeadlinesResponse',
    'message:QueryEvidenceRequest',
    'message:QueryEvidenceResponse',
    'message:QueryGovernanceRequest',
    'message:QueryGovernanceResponse',
    'message:QueryPolicyRequest',
    'message:QueryPolicyResponse',
    'message:QueryProviderRequest',
    'message:QueryProviderResponse',
    'message:QueryProvidersRequest',
    'message:QueryProvidersResponse',
    'message:ReconciliationRecord',
    'message:SafetyCaps',
    'message:Submitter',
    'message:Tombstone',
    'message:ValidatorPower',
    'message:ValidatorPowerChange',
    'oneof:change',
    'oneof:point',
    'rpc:AcceptLease',
    'rpc:ActiveLeases',
    'rpc:AdmitProvider',
    'rpc:ApproveChange',
    'rpc:Content',
    'rpc:Deadlines',
    'rpc:DisableProvider',
    'rpc:DrainProvider',
    'rpc:EndLease',
    'rpc:Evidence',
    'rpc:ExecuteChange',
    'rpc:ForceWithdraw',
    'rpc:Governance',
    'rpc:OfferLease',
    'rpc:Policy',
    'rpc:ProposeChange',
    'rpc:Provider',
    'rpc:Providers',
    'rpc:RegisterContent',
    'rpc:RegisterProvider',
    'rpc:RenewLease',
    'rpc:RotateProviderKey',
    'rpc:SubmitAuditBatch',
    'rpc:UpdateProvider',
    'rpc:WithdrawContent',
    'service:Msg',
    'service:Query',
)

REQUIRED_HEADINGS = [
    "## Freeze",
    "## Hard exclusions",
    "## Trust boundaries",
    "## Identifiers, integers, time, and digests",
    "## Signature domains",
    "## Error codes",
    "## Open decision parameters",
    "## Canonicalization",
    "## Chain transaction envelope",
    "## State machines",
    "## Transitions",
    "## Expiry and determinism",
    "## Privacy, tombstones, and reconciliation",
    "## Off-chain records",
    "## API contracts",
    "## Genesis",
    "## Page 26 failure tests",
    "## Invariants and implementation",
    "## Handoff",
]
REQUIRED_PHRASES = [
    "Public launch: DISABLED",
    "not an approved protocol",
    "frozen-pending-Gate-A",
    "Hashing is not anonymization",
    "SIGN_MODE_DIRECT",
    "SVN-JCS-1",
    "RFC 8785",
    "x/mint",
    "x/distribution",
    "x/slashing",
    "MsgCreateValidator",
    "Committed chain time",
    "local clock",
    "ATTESTATION_NOT_STORAGE_PROOF",
    "LEASE_STATE_AT_RISK",
    "REACHABILITY_DEGRADED",
    "history_erased",
    "DEC-001",
    "DEC-008",
    "PROPOSED_TEST_PARAMETER",
]
REQUIRED_CASES = {
    "boundary-id-64",
    "boundary-id-65",
    "malformed-missing-field",
    "unknown-field",
    "duplicate-lease",
    "wrong-domain",
    "expired-offer",
    "unauthorized-signer",
    "forged-signature",
    "replay-same-business",
    "replay-different-payload",
    "stall-one-validator",
    "stall-two-validators",
    "corrupt-manifest",
    "replica-match",
    "withdrawn-blocks-offer",
    "withdrawn-blocks-revival",
    "underflow-double-end",
    "overflow-reserve",
    "stale-term-version",
    "conflict-governance",
    "evidence-expired-cannot-renew",
    "evidence-does-not-extend-lease",
    "decision-blocked-without-devnet",
    "devnet-does-not-approve-decision",
    "no-local-clock-field",
    "zero-fee",
    "nonzero-fee",
    "reject-msg-create-validator",
    "reject-mint",
    "publication-separate",
    "reachability-not-lease-state",
    "capacity-exceeded",
    "idempotent-retry-no-double-reserve",
    "bounded-expiry",
    "drain-stops-new-offers",
    "rotate-key-stale-generation",
    "storage-proof-rejected",
    "host-custody-not-assumed",
    "renew-succeeds",
    "renew-changed-bytes",
    "update-provider",
    "update-provider-below-reserved",
    "admit-blocked-by-dec-002",
    "admit-non-authority",
    "second-withdraw",
    "approve-same-seat-replay",
    "propose-stale-base",
    "propose-dec-named-key",
    "execute-before-delay",
    "authority-end-lease",
    "rotate-then-old-key-fails",
    "provider-disabled",
    "proposal-expired",
    "proposal-rejected",
    "validator-power-applied",
    "evidence-rejected",
    "force-withdraw",
    "force-withdraw-non-authority",
    "replay-app-hash",
    "hosting-resilience",
    "renew-manifest-mismatch",
    "governed-limit-over-key-cap",
    "governed-limit-read",
    "duplicate-validator-pubkey",
    "admit-payload-blocked-by-dec-002",
    "execute-admit-provider",
    "execute-disable-provider",
    "execute-force-withdraw",
    "lease-expired",
    "validator-power-time-applied",
    "validator-activation-not-immediate",
    "validator-power-missing-reason",
    "wrong-payload-digest",
    "validator-power-explicit-zero",
    "stacked-validator-removal",
    "validator-removal-breaks-quorum",
    "duplicate-scheduled-validator-key",
}
REQUIRED_VALID_IDS = {
    "empty-object",
    "sort-keys",
    "whitespace",
    "nested",
    "newline",
    "control",
    "unicode",
    "slash",
    "quote",
    "safe-max",
    "safe-min",
    "letter-escape",
    "zero",
    "proposal-payload-validator-power",
    "proposal-payload-validator-power-zero",
}
REQUIRED_INVALID_IDS = {
    "null",
    "fraction",
    "exponent",
    "leading-zero",
    "negative-zero",
    "unsafe-integer",
    "duplicate-key",
    "trailing-comma",
    "bad-key",
    "unicode-key",
    "lone-surrogate",
    "above-int64",
    "bom",
    "invalid-utf8",
}
CLIENT_PATHS = [
    "/upload-intents",
    "/uploads/{upload_id}",
    "/publish",
    "/feeds",
    "/reports",
    "/removal",
    "/exports",
]
PROVIDER_PATHS = [
    "/capability",
    "/offers/{offer_id}/acceptance",
    "/packages/{content_id}/{content_version}",
    "/transfers/{transfer_id}/receipt",
    "/inventory",
]
REQUIRED_EVENTS = [
    "ContentRegistered",
    "LeaseAccepted",
    "LeaseRenewed",
    "LeaseEnded",
    "EvidenceAccepted",
    "PolicyChanged",
]

def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate_schema(instance: object, schema: dict, path: str = "$") -> list[str]:
    errors: list[str] = []
    if "const" in schema and instance != schema["const"]:
        errors.append(f"{path} const")
    if "enum" in schema and instance not in schema["enum"]:
        errors.append(f"{path} enum")
    kind = schema.get("type")
    if kind == "object":
        if not isinstance(instance, dict):
            return errors + [f"{path} type"]
        properties = schema.get("properties", {})
        if schema.get("additionalProperties") is False:
            for key in instance:
                if key not in properties:
                    errors.append(f"{path}.{key} unknown")
        for key in schema.get("required", []):
            if key not in instance:
                errors.append(f"{path}.{key} missing")
        for key, sub in properties.items():
            if key in instance:
                errors.extend(validate_schema(instance[key], sub, f"{path}.{key}"))
    elif kind == "string":
        if not isinstance(instance, str):
            return errors + [f"{path} type"]
        if "minLength" in schema and len(instance) < schema["minLength"]:
            errors.append(f"{path} minLength")
        if "maxLength" in schema and len(instance) > schema["maxLength"]:
            errors.append(f"{path} maxLength")
        if "pattern" in schema and re.fullmatch(schema["pattern"], instance) is None:
            errors.append(f"{path} pattern")
    elif kind == "integer":
        if isinstance(instance, bool) or not isinstance(instance, int):
            return errors + [f"{path} type"]
        if "minimum" in schema and instance < schema["minimum"]:
            errors.append(f"{path} minimum")
        if "maximum" in schema and instance > schema["maximum"]:
            errors.append(f"{path} maximum")
    elif kind == "boolean":
        if not isinstance(instance, bool):
            errors.append(f"{path} type")
    elif kind == "array":
        if not isinstance(instance, list):
            return errors + [f"{path} type"]
        if "minItems" in schema and len(instance) < schema["minItems"]:
            errors.append(f"{path} minItems")
        if "maxItems" in schema and len(instance) > schema["maxItems"]:
            errors.append(f"{path} maxItems")
        item_schema = schema.get("items")
        if isinstance(item_schema, dict):
            for index, item in enumerate(instance):
                errors.extend(validate_schema(item, item_schema, f"{path}[{index}]"))
    return errors


def check_text(protocol: str, decisions: str) -> list[str]:
    errors: list[str] = []
    for heading in REQUIRED_HEADINGS:
        if heading not in protocol:
            errors.append(f"protocol missing {heading}")
    for phrase in REQUIRED_PHRASES:
        if phrase not in protocol:
            errors.append(f"protocol missing {phrase!r}")
    if "Status: APPROVED" in protocol:
        errors.append("protocol marks a status approved")
    for decision_id in [f"DEC-{i:03d}" for i in range(1, 9)]:
        if decision_id not in protocol:
            errors.append(f"protocol missing {decision_id}")
        errors.extend(open_decision(decisions, decision_id))
    for decision_id in ("DEC-009", "DEC-010"):
        if decision_id not in protocol:
            errors.append(f"protocol missing {decision_id}")
        errors.extend(open_decision(decisions, decision_id))
    if cases.REPLAY_GOLDEN not in protocol:
        errors.append("protocol missing replay golden")
    if PAYLOAD_DIGEST not in protocol:
        errors.append("protocol missing payload digest")
    if PAYLOAD_ZERO_DIGEST not in protocol:
        errors.append("protocol missing zero-power payload digest")
    return errors


def open_decision(decisions: str, decision_id: str) -> list[str]:
    if f"## {decision_id}" not in decisions:
        return [f"register missing {decision_id}"]
    section = decisions.split(f"## {decision_id}", 1)[1].split("\n## ", 1)[0]
    errors: list[str] = []
    if "Status: OPEN" not in section:
        errors.append(f"{decision_id} is not OPEN")
    if re.search(r"^Status: APPROVED\s*$", section, re.M):
        errors.append(f"{decision_id} is APPROVED")
    return errors


def check_protos(bounds: dict) -> list[str]:
    errors: list[str] = []
    protos = sorted(PROTO_ROOT.rglob("*.proto"))
    if len(protos) < 20:
        errors.append("too few proto files")
    text = "\n".join(path.read_text(encoding="utf-8") for path in protos)
    if "map<" in text:
        errors.append("proto uses a map")
    if "message MsgCreateValidator" in text:
        errors.append("MsgCreateValidator is defined")
    for name in REQUIRED_EVENTS + ["AppGenesis", "BlockedDecisionParameter", "Tombstone", "MsgAcceptLease"]:
        if f"message {name} " not in text and f"message {name}{{" not in text:
            errors.append(f"proto missing {name}")
    for domain in bounds["domains"].values():
        if domain not in (ROOT / "docs" / "protocol.md").read_text(encoding="utf-8"):
            errors.append(f"protocol missing domain {domain}")
    if tuple(bounds.get("forbidden_name_patterns", ())) != PINNED_FORBIDDEN_PATTERNS:
        errors.append("forbidden pattern pin")
    if tuple(bounds.get("forbidden_name_exemptions", ())) != PINNED_NAME_EXEMPTIONS:
        errors.append("forbidden exemption pin")
    if bounds.get("governed_limit_caps") != PINNED_GOVERNED_CAPS:
        errors.append("governed limit cap pin")
    if not protoc_has_well_known_types():
        print("protoc well-known types BLOCKED")
        errors.append("protoc well-known types BLOCKED")
        return errors
    try:
        with tempfile.TemporaryDirectory(prefix="svn-protocol-") as tmp:
            descriptor_set = Path(tmp) / "protocol.pb"
            completed = subprocess.run(
                [
                    "protoc",
                    f"--proto_path={PROTO_ROOT}",
                    f"--descriptor_set_out={descriptor_set}",
                    *[str(path) for path in protos],
                ],
                cwd=ROOT,
                capture_output=True,
                text=True,
                check=False,
            )
            print(f"protoc exit:{completed.returncode}")
            if completed.returncode != 0:
                errors.append(f"protoc failed {completed.stderr.strip()}")
            elif completed.stderr.strip():
                errors.append(f"protoc warning {completed.stderr.strip()}")
            else:
                errors.extend(scan_real_descriptor(decode_descriptor(descriptor_set)))
                errors.extend(scan_negative_descriptors(Path(tmp)))
    except FileNotFoundError:
        print("protoc BLOCKED")
        errors.append("protoc BLOCKED")
    return errors


def protoc_has_well_known_types() -> bool:
    """Ask protoc itself for google/protobuf/descriptor.proto. No include path is passed."""
    if shutil.which("protoc") is None:
        return False
    completed = subprocess.run(
        ["protoc", "--descriptor_set_out=/dev/null", "google/protobuf/descriptor.proto"],
        capture_output=True,
        check=False,
    )
    return completed.returncode == 0


def decode_descriptor(pb_path: Path) -> str:
    completed = subprocess.run(
        [
            "protoc",
            "--decode=google.protobuf.FileDescriptorSet",
            "google/protobuf/descriptor.proto",
        ],
        input=pb_path.read_bytes(),
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        raise RuntimeError(completed.stderr.decode())
    return completed.stdout.decode()


def pattern_matches(name: str, pattern: str) -> bool:
    folded = name.casefold()
    needle = pattern.casefold()
    if needle == "ip":
        if re.search(r"(?:^|_)ip(?:_|$)", folded):
            return True
        return "ipv4" in folded or "ipv6" in folded or "ipaddress" in folded
    if needle == "addr":
        return re.search(r"(?:^|_)addr(?:_|$)", folded) is not None
    return needle in folded


def descriptor_symbols(text: str) -> list[tuple[str, str]]:
    """Message, field, enum, rpc, and json names. File paths and packages are not symbols."""
    symbols: list[tuple[str, str]] = []
    stack = ["root"]
    field_name: str | None = None
    block_kinds = {
        "message_type": "message",
        "enum_type": "enum",
        "value": "enum_value",
        "field": "field",
        "oneof_decl": "oneof",
        "service": "service",
        "method": "rpc",
    }
    for raw in text.splitlines():
        line = raw.strip()
        if line.endswith("{") and ": " not in line.split("{", 1)[0]:
            stack.append(line[:-1].strip())
            if stack[-1] == "field":
                field_name = None
            continue
        if line == "}":
            if len(stack) > 1:
                stack.pop()
            continue
        parent = stack[-1]
        if line.startswith("name: "):
            value = line.split(": ", 1)[1].strip().strip('"')
            if parent == "file":
                continue
            kind = block_kinds.get(parent)
            if kind is None:
                continue
            symbols.append((kind, value))
            if parent == "field":
                field_name = value
            continue
        if line.startswith("json_name: ") and parent == "field":
            value = line.split(": ", 1)[1].strip().strip('"')
            symbols.append(("json_name", value))
            continue
        if parent == "field" and field_name is not None:
            if "google.protobuf.Timestamp" in line:
                symbols.append(("timestamp", field_name))
            if "TYPE_DOUBLE" in line or "TYPE_FLOAT" in line:
                symbols.append(("float", field_name))
    return symbols


def descriptor_name_tokens(symbols: list[tuple[str, str]]) -> set[str]:
    return {f"{kind}:{name}" for kind, name in symbols if kind in NAME_KINDS}


def labels_by_symbol(
    symbols: list[tuple[str, str]],
    patterns: tuple[str, ...],
    exemptions: tuple[str, ...],
) -> dict[str, set[str]]:
    """Pattern hits keyed by the field or message that matched. Packages do not count."""
    if not patterns:
        return {"": {"empty-pattern-list"}}
    found: dict[str, set[str]] = {}
    exempt = set(exemptions)
    for kind, name in symbols:
        if kind in {"timestamp", "float"}:
            found.setdefault(name, set()).add(kind)
            continue
        if kind not in NAME_KINDS or name in exempt:
            continue
        labels = {pattern for pattern in patterns if pattern_matches(name, pattern)}
        if labels:
            found.setdefault(name, set()).update(labels)
    return found


def name_hits(text: str, patterns: tuple[str, ...], exemptions: tuple[str, ...]) -> set[str]:
    labels = labels_by_symbol(descriptor_symbols(text), patterns, exemptions)
    hits: set[str] = set()
    for group in labels.values():
        hits.update(group)
    return hits


def missing_declared_names(text: str, required: tuple[str, ...]) -> list[str]:
    return [name for name in required if f'name: "{name}"' not in text]


def allowlist_errors(tokens: set[str], pinned: tuple[str, ...]) -> list[str]:
    if not pinned:
        return ["descriptor allowlist is empty"]
    extra = sorted(tokens - set(pinned))
    missing = sorted(set(pinned) - tokens)
    if extra or missing:
        return [f"descriptor allowlist extra {extra} missing {missing}"]
    return []


def scan_real_descriptor(text: str) -> list[str]:
    symbols = descriptor_symbols(text)
    errors = allowlist_errors(descriptor_name_tokens(symbols), PINNED_DESCRIPTOR_NAMES)
    labels = labels_by_symbol(symbols, PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS)
    hit_names = sorted(label for group in labels.values() for label in group)
    errors.extend(f"descriptor hit {hit}" for hit in hit_names)
    declared = {name for _kind, name in symbols}
    for name in PINNED_ONEOF_MEMBERS:
        if name not in declared:
            errors.append(f"oneof member missing {name}")
    for name in PINNED_RPCS:
        if name not in declared:
            errors.append(f"rpc missing {name}")
    for name in (
        "MsgAdmitProvider",
        "MsgForceWithdraw",
        "MsgDisableProvider",
        "ProposalPayload",
        "ValidatorPowerChange",
        "ActivationPoint",
    ):
        if name not in declared:
            errors.append(f"descriptor missing {name}")
    return errors


def negative_requirements() -> dict[str, list[tuple[str, str]]]:
    leaks = [
        ("ipv4_address", "ip"),
        ("ipAddress", "ip"),
        ("remote_addr", "addr"),
        ("client_ipv6", "ip"),
        ("device_id", "device"),
        ("geo_location", "geo"),
        ("viewer_history", "viewer"),
        ("payout_amount", "payout"),
        ("bond_amount", "bond"),
        ("fee_amount", "fee"),
        ("coin_denom", "coin"),
        ("inflation_rate", "inflation"),
        ("issuance", "issuance"),
        ("media_blob", "media"),
        ("chunk_data", "chunk"),
    ]
    return {
        "timestamp.proto": [("local_time", "timestamp")],
        "mint.proto": [("MsgMint", "mint")],
        "slash.proto": [("MsgSlashValidator", "slash")],
        "media.proto": [
            ("video_data", "video_data"),
            ("uploader_ip", "ip"),
            ("score", "float"),
            ("reward_amount", "reward"),
        ],
        "storage_proof.proto": [("EVIDENCE_KIND_STORAGE_PROOF", "storage_proof")],
        "mint_tokens.proto": [("MsgMintTokens", "mint")],
        "reward.proto": [("reward", "reward")],
        "ip_address.proto": [("ip_address", "ip")],
        "video_payload.proto": [("video_payload", "video_payload")],
        "penalize.proto": [("slash_fraction", "slash")],
        "token_balance.proto": [("token_balance", "token"), ("token_balance", "balance")],
        "email.proto": [("email", "email")],
        "leaks.proto": leaks,
    }


def scan_negative_descriptors(tmp: Path) -> list[str]:
    errors: list[str] = []
    negative = Path(__file__).resolve().parent / "negative"
    for name, pairs in negative_requirements().items():
        proto = negative / name
        out = tmp / f"negative-{name}.pb"
        completed = subprocess.run(
            [
                "protoc",
                f"--proto_path={proto.parent}",
                f"--descriptor_set_out={out}",
                str(proto),
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        if completed.returncode != 0:
            errors.append(f"negative proto {name} did not compile")
            continue
        labels = labels_by_symbol(
            descriptor_symbols(decode_descriptor(out)),
            PINNED_FORBIDDEN_PATTERNS,
            PINNED_NAME_EXEMPTIONS,
        )
        for symbol, needle in pairs:
            if needle not in labels.get(symbol, set()):
                errors.append(f"negative {name} missed {symbol} {needle}")
    return errors


def _descriptor_field(name: str) -> str:
    return f'field {{\n  name: "{name}"\n}}\n'


def _descriptor_message(name: str) -> str:
    return f'message_type {{\n  name: "{name}"\n}}\n'


def pin_self_test() -> list[str]:
    """Fail if the pin lists or the pattern matcher are emptied or narrowed."""
    errors: list[str] = []
    if not PINNED_FORBIDDEN_PATTERNS:
        errors.append("forbidden pattern pin is empty")
    for needle in (
        "mint",
        "reward",
        "slash",
        "token",
        "balance",
        "stake",
        "ip",
        "video_data",
        "video_payload",
        "email",
        "addr",
        "device",
        "geo",
        "viewer",
        "payout",
        "bond",
        "fee",
        "coin",
        "inflation",
        "issuance",
        "media",
        "chunk",
    ):
        if needle not in PINNED_FORBIDDEN_PATTERNS:
            errors.append(f"pattern pin missing {needle}")
    if "validator_power" not in PINNED_ONEOF_MEMBERS:
        errors.append("oneof pin dropped validator_power")
    if "AdmitProvider" not in PINNED_RPCS:
        errors.append("rpc pin dropped AdmitProvider")
    if "empty-pattern-list" not in name_hits(_descriptor_field("email"), (), PINNED_NAME_EXEMPTIONS):
        errors.append("empty pattern list passed")
    samples = {
        _descriptor_message("MsgMintTokens"): "mint",
        _descriptor_field("reward"): "reward",
        _descriptor_field("ip_address"): "ip",
        _descriptor_field("ipv4_address"): "ip",
        _descriptor_field("ipAddress"): "ip",
        _descriptor_field("remote_addr"): "addr",
        _descriptor_field("client_ipv6"): "ip",
        _descriptor_field("device_id"): "device",
        _descriptor_field("geo_location"): "geo",
        _descriptor_field("viewer_history"): "viewer",
        _descriptor_field("payout_amount"): "payout",
        _descriptor_field("bond_amount"): "bond",
        _descriptor_field("fee_amount"): "fee",
        _descriptor_field("coin_denom"): "coin",
        _descriptor_field("inflation_rate"): "inflation",
        _descriptor_field("issuance"): "issuance",
        _descriptor_field("media_blob"): "media",
        _descriptor_field("chunk_data"): "chunk",
        _descriptor_field("video_payload"): "video_payload",
        _descriptor_field("slash_fraction"): "slash",
        _descriptor_field("token_balance"): "token",
        _descriptor_field("email"): "email",
    }
    for text, label in samples.items():
        hits = name_hits(text, PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS)
        if label not in hits:
            errors.append(f"pattern missed {label} in {text.splitlines()[1].strip()}")
    balance = name_hits(_descriptor_field("token_balance"), PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS)
    if "balance" not in balance:
        errors.append("pattern missed balance")
    for name in PINNED_NAME_EXEMPTIONS:
        block = _descriptor_field(name) if name != "PRIVACY_CLASS_SECRET" else f'value {{\n  name: "{name}"\n}}\n'
        if name_hits(block, PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS):
            errors.append(f"exemption hit {name}")
    if "validator_power" not in missing_declared_names('name: "governed_limit"\n', PINNED_ONEOF_MEMBERS):
        errors.append("dropped oneof member passed")
    if "AdmitProvider" not in missing_declared_names('name: "RegisterProvider"\n', PINNED_RPCS):
        errors.append("dropped rpc passed")
    if name_hits(_descriptor_field("endpoint_digest"), PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS):
        errors.append("endpoint matched a forbidden pattern")
    if name_hits(_descriptor_field("address"), PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS):
        errors.append("address matched a forbidden pattern")
    if "secret" not in name_hits(_descriptor_field("secret"), PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS):
        errors.append("pattern missed secret field")
    package_only = 'file {\n  name: "negative/mint.proto"\n  package: "negative.mint"\n}\n'
    if name_hits(package_only, PINNED_FORBIDDEN_PATTERNS, PINNED_NAME_EXEMPTIONS):
        errors.append("package line matched a forbidden pattern")
    if not allowlist_errors({"field:ipv4_address"}, ("field:signer",)):
        errors.append("new name passed the allowlist")
    if not allowlist_errors(set(), ("field:signer",)):
        errors.append("empty descriptor matched the allowlist")
    cases_text = (Path(__file__).resolve().parent / "cases.py").read_text(encoding="utf-8")
    l2_assertion = 'if hosts["leases"]["l2"]["reservation_held"] is not True:'
    if l2_assertion not in cases_text:
        errors.append("hosting-resilience l2 assertion missing")
    return errors


def closed_schema_errors(schema: dict, path: str = "$") -> list[str]:
    errors: list[str] = []
    if schema.get("type") == "object":
        if schema.get("additionalProperties") is not False:
            errors.append(f"{path} additionalProperties")
        for key, sub in schema.get("properties", {}).items():
            if isinstance(sub, dict):
                if sub.get("type") == "string" and "maxLength" not in sub and "const" not in sub:
                    errors.append(f"{path}.{key} maxLength")
                errors.extend(closed_schema_errors(sub, f"{path}.{key}"))
    elif schema.get("type") == "array":
        if "maxItems" not in schema:
            errors.append(f"{path} maxItems")
        items = schema.get("items")
        if isinstance(items, dict):
            errors.extend(closed_schema_errors(items, path + "[]"))
    return errors


def segment_ids(manifest: dict) -> list[str]:
    found: list[str] = []
    for rendition in manifest.get("renditions", []):
        for segment in rendition.get("segments", []):
            found.append(segment.get("segment_id", ""))
    return found


def check_contracts(bounds: dict) -> list[str]:
    errors: list[str] = []
    schemas = {}
    for name in (
        "media-manifest.schema.json",
        "provider-capability.schema.json",
        "evidence-envelope.schema.json",
        "public-record.schema.json",
    ):
        schema = json.loads((ROOT / "docs/protocol/schemas" / name).read_text(encoding="utf-8"))
        schemas[name] = schema
        closed = closed_schema_errors(schema, name)
        if closed:
            errors.append(f"{name} not closed: {closed}")
    manifest_schema = schemas["media-manifest.schema.json"]
    valid = json.loads((ROOT / "docs/protocol/fixtures/manifest-valid.json").read_text(encoding="utf-8"))
    if validate_schema(valid, manifest_schema) or len(segment_ids(valid)) != len(set(segment_ids(valid))):
        errors.append("valid manifest rejected")
    fault_files = {
        "manifest-invalid-boundary.json": "schema",
        "manifest-invalid-unknown.json": "schema",
        "manifest-invalid-wrong-domain.json": "schema",
        "manifest-invalid-duplicate-id.json": "duplicate",
    }
    for name, kind in fault_files.items():
        document = json.loads((ROOT / "docs/protocol/fixtures" / name).read_text(encoding="utf-8"))
        schema_errors = validate_schema(document, manifest_schema)
        duplicate = len(segment_ids(document)) != len(set(segment_ids(document)))
        if kind == "schema" and not schema_errors:
            errors.append(f"{name} was accepted")
        if kind == "schema" and duplicate:
            errors.append(f"{name} stacks a duplicate id")
        if kind == "duplicate" and schema_errors:
            errors.append(f"{name} fails schema before the duplicate-id check")
        if kind == "duplicate" and not duplicate:
            errors.append(f"{name} has no duplicate id")
    evidence = schemas["evidence-envelope.schema.json"]
    if "STORAGE_PROOF" in evidence["properties"]["kind"]["enum"]:
        errors.append("evidence schema allows a storage proof kind")
    client = json.loads((ROOT / "docs/protocol/api/client-api.json").read_text(encoding="utf-8"))
    provider = json.loads((ROOT / "docs/protocol/api/provider-api.json").read_text(encoding="utf-8"))
    for path in CLIENT_PATHS:
        if path not in client["paths"]:
            errors.append(f"client API missing {path}")
    if "/register" in client["paths"] or "/signup" in client["paths"]:
        errors.append("client API has a public sign-up route")
    for path in PROVIDER_PATHS:
        if path not in provider["paths"]:
            errors.append(f"provider API missing {path}")
    genesis = json.loads((ROOT / "docs/protocol/fixtures/genesis-valid.json").read_text(encoding="utf-8"))
    errors.extend(check_genesis(genesis, bounds, valid=True))
    minted = json.loads(json.dumps(genesis))
    minted["mint_module_enabled"] = True
    if not check_genesis(minted, bounds, valid=False):
        errors.append("mint genesis was accepted")
    approved = json.loads(json.dumps(genesis))
    approved["blocked_decisions"][0]["approved"] = True
    if not check_genesis(approved, bounds, valid=False):
        errors.append("approved decision genesis was accepted")
    return errors


def check_genesis(doc: dict, bounds: dict, valid: bool) -> list[str]:
    errors: list[str] = []
    for flag in (
        "mint_module_enabled",
        "distribution_module_enabled",
        "slashing_module_enabled",
        "staking_module_enabled",
        "permissionless_validator_creation",
    ):
        if doc.get(flag) is not False:
            errors.append(flag)
    if doc.get("protocol_version") != 1:
        errors.append("protocol_version")
    if doc.get("approved_council"):
        errors.append("approved_council")
    if doc.get("contents") or doc.get("leases") or doc.get("offers"):
        errors.append("nonempty catalogue")
    seen = {row.get("decision_id") for row in doc.get("blocked_decisions", [])}
    if seen != set(bounds["decisions"]):
        errors.append("decision set")
    for row in doc.get("blocked_decisions", []):
        if row.get("approved") or row.get("value_version") != 0 or row.get("schema_version") != 1:
            errors.append(f"{row.get('decision_id')} not blocked")
    for key, value in doc.get("safety_caps", {}).items():
        if bounds.get(key) != value:
            errors.append(f"safety {key}")
    pubs = [row.get("consensus_pubkey_hex") for row in doc.get("validators", [])]
    powers = [row.get("power") for row in doc.get("validators", [])]
    if len(pubs) != len(set(pubs)) or any(not isinstance(item, str) or len(item) != 64 for item in pubs):
        errors.append("validator keys")
    if powers and len(set(powers)) != 1:
        errors.append("validator powers")
    if doc.get("allow_devnet_test_parameters"):
        if not str(doc.get("chain_id", "")).startswith(bounds["devnet_chain_id_prefix"]):
            errors.append("devnet chain id")
        for row in doc.get("devnet_test_parameters", []):
            if row.get("label") != "PROPOSED_TEST_PARAMETER" or row.get("approved_policy"):
                errors.append("devnet test label")
    if "private_key" in json.dumps(doc):
        errors.append("private key")
    if valid and errors:
        return [f"valid genesis: {', '.join(errors)}"]
    if not valid:
        return errors
    return []


def python_vector_report(doc: dict) -> list[str]:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

    lines: list[str] = []
    canonical: dict[str, str] = {}
    for item in doc["valid"]:
        got = jcs.parse_and_canonicalize(item["input_raw"])
        if got != item["canonical"]:
            raise AssertionError(item["id"])
        digest = hashlib.sha256(got.encode("utf-8")).hexdigest()
        lines.append(f"VALID {item['id']} {digest}")
        canonical[item["id"]] = got
    for item in doc["invalid"]:
        try:
            if item.get("raw_hex"):
                jcs.parse_bytes(bytes.fromhex(item["raw_hex"]))
            else:
                jcs.parse_and_canonicalize(item["raw"])
        except jcs.JCSError as exc:
            if exc.reason != item["reason"]:
                raise AssertionError(item["id"]) from exc
        else:
            raise AssertionError(item["id"])
        lines.append(f"INVALID {item['id']} {item['reason']}")
    safe_max = 9007199254740991
    uint64_max = 18446744073709551615
    for item in doc["integers"]:
        value = int(item["decimal"])
        safe = abs(value) <= safe_max
        fits = value >= 0 and value <= uint64_max
        if safe != item["safe"] or fits != item["uint64"]:
            raise AssertionError(item["id"])
        lines.append(f"INTEGER {item['id']} {'safe' if safe else 'unsafe'} {'uint64' if fits else 'over'}")
    for item in doc["arithmetic"]:
        left = int(item["a"])
        right = int(item["b"])
        if item["op"] == "add":
            result = "overflow" if left > uint64_max - right else str(left + right)
        else:
            result = "underflow" if left < right else str(left - right)
        if result != item["result"]:
            raise AssertionError(item["id"])
        lines.append(f"ARITH {item['id']} {result}")
    signer = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(doc["rfc8032_seed"]))
    empty = signer.sign(b"").hex()
    if empty != doc["rfc8032_empty_signature"]:
        raise AssertionError("rfc8032")
    lines.append(f"RFC8032 {empty}")
    for item in doc["sign"]:
        body = canonical[item["vector_id"]].encode("utf-8")
        signed = item["domain"].encode("utf-8") + b"\x00" + body
        signature = signer.sign(signed).hex()
        lines.append(f"SIGN {item['id']} {hashlib.sha256(signed).hexdigest()} {signature}")
    return lines


def run_language(command: list[str]) -> tuple[int, str]:
    try:
        completed = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, check=False)
    except FileNotFoundError:
        return 127, ""
    return completed.returncode, completed.stdout


def compare_languages(python_lines: list[str], go_text: str, ts_text: str) -> list[str]:
    """Require the three reports to be the same list, in both directions."""
    go_lines = [line for line in go_text.splitlines() if line]
    ts_lines = [line for line in ts_text.splitlines() if line]
    errors: list[str] = []
    if python_lines != go_lines:
        errors.append("python and go vector reports differ")
    if python_lines != ts_lines:
        errors.append("python and typescript vector reports differ")
    if go_lines != ts_lines:
        errors.append("go and typescript vector reports differ")
    return errors


def source_clock_errors() -> list[str]:
    errors: list[str] = []
    for name in ("spec_machine.py", "jcs.py", "cases.py"):
        text = (Path(__file__).resolve().parent / name).read_text(encoding="utf-8")
        for token in ("time.time", "datetime", "requests.", "urllib", "socket.", "random."):
            if token in text:
                errors.append(f"{name} contains {token}")
    return errors


def input_files() -> list[Path]:
    files = [
        PROTOCOL,
        PROGRESS,
        PROGRESS_TSV,
        DECISIONS,
        ROOT / "docs" / "requirements.md",
        ROOT / "docs" / "evidence" / "goal-01" / "check_requirements.py",
        Path(__file__).resolve(),
    ]
    files.extend(sorted((ROOT / "docs" / "protocol").rglob("*")))
    files.extend(sorted((ROOT / "docs" / "evidence" / "goal-02").rglob("*")))
    unique: list[Path] = []
    seen: set[Path] = set()
    for path in files:
        if path.is_file() and path not in seen and path.name != "goal-02.md" and "__pycache__" not in path.parts and path.suffix != ".pyc":
            seen.add(path)
            unique.append(path)
    return sorted(unique)


def main() -> int:
    errors: list[str] = []
    errors.extend(pin_self_test())
    bounds = json.loads(BOUNDS_PATH.read_text(encoding="utf-8"))
    protocol = PROTOCOL.read_text(encoding="utf-8")
    decisions = DECISIONS.read_text(encoding="utf-8")
    errors.extend(check_text(protocol, decisions))
    errors.extend(check_protos(bounds))
    errors.extend(check_contracts(bounds))
    errors.extend(source_clock_errors())
    results = cases.cases()
    problems = [case_id for case_id, detail in results if detail]
    ran = [case_id for case_id, _detail in results]
    ran_set = set(ran)
    if len(ran) != len(ran_set):
        errors.append("duplicate transition case id")
    if ran_set != REQUIRED_CASES or len(results) != len(REQUIRED_CASES):
        missing = REQUIRED_CASES - ran_set
        extra = ran_set - REQUIRED_CASES
        errors.append(f"case pin missing {sorted(missing)} extra {sorted(extra)} count {len(results)}")
    if problems:
        errors.append("failed cases " + ", ".join(problems))
    print(f"transition_cases {len(results)}")
    print(f"transition_case_pin {len(REQUIRED_CASES)}")
    print(f"transition_failures {len(problems)}")
    doc = json.loads(VECTORS.read_text(encoding="utf-8"))
    valid_ids = {item["id"] for item in doc["valid"]}
    invalid_ids = {item["id"] for item in doc["invalid"]}
    if valid_ids != REQUIRED_VALID_IDS or len(doc["valid"]) != len(REQUIRED_VALID_IDS):
        errors.append(f"valid vector pin {len(doc['valid'])}")
    if invalid_ids != REQUIRED_INVALID_IDS or len(doc["invalid"]) != len(REQUIRED_INVALID_IDS):
        errors.append(f"invalid vector pin {len(doc['invalid'])}")
    if len(doc["integers"]) != 6 or len(doc["arithmetic"]) != 5:
        errors.append("integer or arithmetic vector count")
    try:
        python_lines = python_vector_report(doc)
    except (AssertionError, jcs.JCSError, KeyError, ValueError) as exc:
        errors.append(f"python vectors {exc}")
        python_lines = []
    go_code, go_out = run_language(["go", "run", str(ROOT / "docs/evidence/goal-02/go/vectorcheck.go"), str(VECTORS)])
    ts_code, ts_out = run_language(
        ["node", "--experimental-strip-types", str(ROOT / "docs/evidence/goal-02/ts/vectorcheck.ts"), str(VECTORS)]
    )
    print(f"go_vectors exit:{go_code}")
    print(f"typescript_vectors exit:{ts_code}")
    languages_match = False
    if go_code == 127:
        print("go BLOCKED")
        errors.append("go BLOCKED")
    elif go_code != 0:
        errors.append(f"go exit {go_code}")
    if ts_code == 127:
        print("typescript BLOCKED")
        errors.append("typescript BLOCKED")
    elif ts_code != 0:
        errors.append(f"typescript exit {ts_code}")
    if go_code == 0 and ts_code == 0 and python_lines:
        language_errors = compare_languages(python_lines, go_out, ts_out)
        errors.extend(language_errors)
        languages_match = not language_errors
    print(f"valid_vectors {len(doc['valid'])}")
    print(f"invalid_vectors {len(doc['invalid'])}")
    print(f"integer_vectors {len(doc['integers'])}")
    print(f"arithmetic_vectors {len(doc['arithmetic'])}")
    print("hashes")
    for path in input_files():
        print(f"{sha256_file(path)}  {path.relative_to(ROOT).as_posix()}")
    print(f"errors {len(errors)}")
    for error in errors:
        print(f"ERROR {error}")
    if errors:
        print("result FAIL")
        return 1
    if languages_match:
        print("languages MATCH")
    print("result PASS")
    print("note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled")
    return 0


if __name__ == "__main__":
    sys.exit(main())
