// Package provider is the storage-provider agent slot.
//
// The agent itself is cmd/svn-provider. It enrolls a development key, signs
// capability manifests and lease acceptances, stores packages it has checked,
// and serves them over HTTPS only after it has observed a committed lease.
// Scaffold is false. This package does not open a port.
package provider

// Scaffold is false once cmd/svn-provider is the agent. Goal 03's placeholder
// kept it true so an empty directory could not be mistaken for an agent.
const Scaffold = false
