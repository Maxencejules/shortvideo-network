// Package faultpoint marks the place where a devnet test kills the indexer
// after it has written a block's projection rows and before it has advanced
// the checkpoint. Both writes sit in one database transaction, so killing
// the process rolls the rows back.
//
// Only a binary built with svn_faultpoint arms the point. The default build
// inlines Hit to nothing.
package faultpoint

// AfterEffectsBeforeCheckpoint is the fault point between projection writes
// and the checkpoint update.
const AfterEffectsBeforeCheckpoint = "after-effects-before-checkpoint"

// EnvDir is the arm-file directory. A default build never reads it.
const EnvDir = "SVN_FAULTPOINT_DIR"
