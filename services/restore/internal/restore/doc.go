// Package restore backs up and restores application state for the devnet beta.
//
// A backup is a directory of parts plus a manifest. The database dump, the
// signed records, and the media bytes are encrypted. The encryption key is
// not written into that directory. Signer metadata records the consensus
// address and the last height, round, and step. It does not contain a
// signing key.
//
// Retention, custody, and recovery policy are unset. The placeholders are
// labelled. Nothing in this package sets a retention period or a service
// recovery target.
package restore
