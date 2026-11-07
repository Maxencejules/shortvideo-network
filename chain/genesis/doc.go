// Package genesis validates the runnable genesis document and builds the
// CometBFT genesis file from it.
//
// The document is scaffolding for Goal 05. It carries the field inventory from
// svn.app.v1.AppGenesis: chain id, time, protocol version, the disabled
// economic modules, initial authorities, public validator keys, equal voting
// powers, admitted application accounts, and resource bounds. It is not the
// provider, content, lease, evidence, or policy machine. A launch document
// keeps the provider and content catalogue, leases, offers, and evidence
// empty; x/providers, x/content, x/leases, and x/evidence fill their stores
// from committed transactions. The rule that two AUDITOR submitters must not
// share an evidence id prefix is an app genesis rule (chain/app), not a
// document rule.
//
// Private keys are rejected. This package does not generate them.
//
// Validate requires at least MinValidators validators with distinct node ids
// and keys, equal positive powers, and a total power of at most
// SafeIntegerMax. The power rules are the spec validate_genesis_view check.
// Two council seat rules are stricter than the spec, as in x/authority's
// genesis check: a seat id has at most MaxSeatIDLength characters (D-10),
// and no seat key is a validator consensus key (D-7).
package genesis
