// Package genesis validates the runnable genesis document and builds the
// CometBFT genesis file from it.
//
// The document is scaffolding for Goal 05. It carries the field inventory from
// svn.app.v1.AppGenesis: chain id, time, protocol version, the disabled
// economic modules, initial authorities, public validator keys, equal voting
// powers, admitted application accounts, and resource bounds. It is not the
// provider, content, lease, evidence, or policy machine. Those stay empty
// until Goals 06-10.
//
// Private keys are rejected. This package does not generate them.
package genesis
