// Package genesis validates the runnable genesis document and builds the
// CometBFT genesis file from it.
//
// The document is scaffolding for Goal 05. It carries the field inventory from
// svn.app.v1.AppGenesis: chain id, time, protocol version, the disabled
// economic modules, initial authorities, public validator keys, equal voting
// powers, admitted application accounts, and resource bounds. It is not the
// provider, content, lease, evidence, or policy machine. A launch document
// keeps the provider and content catalogue, leases, offers, and evidence
// empty; x/providers and x/content fill their registries from committed
// transactions.
//
// Private keys are rejected. This package does not generate them.
//
// Validate requires at least MinValidators validators with distinct node ids
// and keys, equal positive powers, and a total power of at most
// SafeIntegerMax. The power rules are the spec validate_genesis_view check.
package genesis
