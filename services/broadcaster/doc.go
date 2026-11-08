// Package broadcaster is the root of the services/broadcaster Go module,
// which is to hold the transaction broadcaster and its PostgreSQL outbox.
// This package declares no identifiers. The module's go.mod pins every
// requirement to the version that chain/go.mod requires, so the service
// packages can import the chain module without changing the versions the
// chain selects.
package broadcaster
