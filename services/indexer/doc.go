// Package indexer is the goal 12 committed-state indexer.
//
// The module builds restartable PostgreSQL projections from committed
// blocks and reconciles them against chain queries. The database is a
// read model. It is not a chain, and a row in it is not a publication
// approval or a lease renewal.
package indexer
