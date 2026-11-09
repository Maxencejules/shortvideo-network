package outbox

import (
	"errors"
	"fmt"
	"math"
)

// Limits are the PROPOSED numbers of plan section 11 (DR-11-10) that the
// state machine's guards use. Their values come from configuration; this
// package holds only the hard bounds, which Validate enforces. None of the
// values is an owner decision.
type Limits struct {
	// IndexingMargin is L, in blocks: the dead rule (d3) waits until the
	// committed height is at least timeout_height + L before it trusts a
	// "not found" from /tx and the acc_seq search, and the effect check
	// waits until its read height is L blocks above the height at which it
	// first saw an object changed. L is at least MinIndexingMargin.
	IndexingMargin uint64
	// AttemptsPerBusinessID bounds the attempts persisted under one
	// business id.
	AttemptsPerBusinessID int
	// BusinessAttempts bounds the business ids one intent may open; a new
	// one opens only after a committed REPLAY with no effect (plan 8.6
	// step 5).
	BusinessAttempts int
	// AttemptsPerIntent bounds the attempts persisted for one intent across
	// all its business ids.
	AttemptsPerIntent int
	// Sdk4Retries bounds the attempts that may die by (d2) with a CheckTx
	// refusal of sdk code 4 before the intent fails (DR-11-18).
	Sdk4Retries int
}

// Hard bounds of the Limits fields (plan section 11). BusinessAttempts and
// AttemptsPerIntent also stay within the outbox schema's column bounds
// (business_attempt 1-8, attempt_count 0-32).
//
// MinIndexingMargin is 2 because of CometBFT v0.40.0's event ordering: a
// node in consensus whose block store or application reports height S has
// indexed every block up to S-2, and block S-1 may not be indexed yet. The
// event bus and the indexer's subscriptions are unbuffered
// (types/event_bus.go:13, libs/pubsub/pubsub.go:119 and :418-420,
// state/txindex/indexer_service.go:47,55); the indexer reads block H+1's
// events only after it wrote block H; and consensus publishes the new
// round step (consensus/state.go:753,767, in updateToState after
// ApplyVerifiedBlock) of height H+1 only after the indexer took block
// H+1's events, and saves block H+2 after that. So a height of at least
// H+2 means block H is indexed: L = 2 holds by this ordering, and L = 1
// could trust a search that has not seen block H yet. Block sync publishes
// no round step; a catching-up node is outside this argument.
const (
	MinIndexingMargin        = 2
	MaxIndexingMargin        = 20
	MinAttemptsPerBusinessID = 1
	MaxAttemptsPerBusinessID = 16
	MinBusinessAttempts      = 1
	MaxBusinessAttempts      = 8
	MinAttemptsPerIntent     = 1
	MaxAttemptsPerIntent     = 32
	MinSdk4Retries           = 1
	MaxSdk4Retries           = 10
)

// MaxHeight bounds every height and timeout height this package accepts,
// so timeout_height + IndexingMargin never overflows. CometBFT heights are
// int64; real chains stay far below this bound.
const MaxHeight uint64 = 1 << 62

// MaxSequence bounds every account sequence this package accepts.
const MaxSequence uint64 = 1 << 62

// MaxSends bounds an attempt's send counters at the outbox schema's
// smallint column limit.
const MaxSends = math.MaxInt16

// Validate checks every field against its hard bounds.
func (l Limits) Validate() error {
	var errs []error
	check := func(name string, v, lo, hi int) {
		if v < lo || v > hi {
			errs = append(errs, fmt.Errorf("outbox: %s %d outside [%d, %d]", name, v, lo, hi))
		}
	}
	if l.IndexingMargin < MinIndexingMargin || l.IndexingMargin > MaxIndexingMargin {
		errs = append(errs, fmt.Errorf("outbox: indexing margin %d outside [%d, %d]",
			l.IndexingMargin, MinIndexingMargin, MaxIndexingMargin))
	}
	check("attempts per business id", l.AttemptsPerBusinessID, MinAttemptsPerBusinessID, MaxAttemptsPerBusinessID)
	check("business attempts", l.BusinessAttempts, MinBusinessAttempts, MaxBusinessAttempts)
	check("attempts per intent", l.AttemptsPerIntent, MinAttemptsPerIntent, MaxAttemptsPerIntent)
	check("sdk 4 retries", l.Sdk4Retries, MinSdk4Retries, MaxSdk4Retries)
	return errors.Join(errs...)
}
