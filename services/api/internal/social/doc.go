// Package social is the off-chain discovery service for follows, comments,
// private saves, search, and feeds.
//
// Ordinary social actions are not chain transactions. Saved videos, private
// preferences, and raw viewing histories are not public records. Follow and
// unfollow are the only public-record types this service accepts, and only
// when the account presents a signature this process can verify. It does not
// hold a signing key.
//
// State is in memory. A PostgreSQL store is not wired: this workspace has no
// PostgreSQL server, and the account schema is a separate process.
package social
