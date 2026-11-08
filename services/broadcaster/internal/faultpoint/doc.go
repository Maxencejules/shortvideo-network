// Package faultpoint holds the named fault points of the broadcaster (goal 11
// plan 8.3 and 13.4). A fault point marks a place in the submission path
// where a devnet test kills the process to show that the outbox recovers:
//
//   - AfterSignBeforePersist: the signer returned the bytes, nothing is
//     stored yet;
//   - AfterPersistCommit: the attempt row is committed, nothing is counted
//     as sent;
//   - AfterSendsStarted: the send is counted (sends_started), nothing is
//     sent;
//   - AfterBroadcastBeforeRecord: broadcast_tx_sync answered, the outcome is
//     not recorded;
//   - AfterDecodeBeforeReply: in the signer process, the request is signed
//     and the reply is not written.
//
// Only a binary built with the build tag svn_faultpoint has working fault
// points. In such a binary a point is armed by a file named "<point>.arm" in
// the directory that the environment variable SVN_FAULTPOINT_DIR names. The
// first time an armed point is reached, the arm file is renamed to
// "<point>.hit" (the marker the test waits for) and the calling goroutine
// blocks for the rest of the process's life; the test then reads the
// database and sends SIGKILL. The rename makes each arming fire once, so a
// restarted process with the same directory runs past the point.
//
// The default build has no fault points: Hit is an empty function the
// compiler inlines away and Enabled is false, so no fault point code or
// symbol is linked (TestDefaultBuildHasNoFaultPoints checks the binary).
package faultpoint

// Fault point names (plan 8.3).
const (
	AfterSignBeforePersist     = "after-sign-before-persist"
	AfterPersistCommit         = "after-persist-commit"
	AfterSendsStarted          = "after-sends-started"
	AfterBroadcastBeforeRecord = "after-broadcast-before-record"
	AfterDecodeBeforeReply     = "after-decode-before-reply"
)

// EnvDir names the environment variable that holds the arm directory of a
// fault-point build. A default build never reads it.
const EnvDir = "SVN_FAULTPOINT_DIR"
