package outbox

// retry finishes T13 and T14 after the effect check found no effect (plan
// 8.6 step 5, 8.7). step.ID is T13 for a committed failure and T14 for a
// dead attempt; step.Attempt is the attempt's resolution.
//
// Committed failures (T13):
//   - EXECUTION_FAILED with svn REPLAY: the business id was taken by another
//     payload; a new business attempt opens, bounded (BUSINESS_ID_TAKEN at
//     the bound);
//   - any other EXECUTION_FAILED: FAILED(EXECUTION_FAILED{code});
//   - INCLUDED_ANTE_FAILED with sdk 30 or 42 (deadline), 32 (sequence race)
//     or 11 (out of block gas): retried under the same business id;
//   - any other INCLUDED_ANTE_FAILED: FAILED(EXECUTION_FAILED{code}).
//
// Dead attempts (T14):
//   - (d2), decided by the last recorded refusal: a node refusal is retried;
//     sdk 4 is retried until more than Sdk4Retries attempts died with it,
//     each retry at least Sdk4RetryGapBlocks later, then
//     FAILED(REJECTED{sdk 4}) with an alert; svn UNAUTHORIZED is
//     FAILED(REJECTED) and halts the signer; every other CheckTx code
//     (svn FORBIDDEN, BOUNDS, FEE_REJECTED, sdk 2 and any code not named
//     here) is FAILED(REJECTED{code});
//   - (d3): retried under the same business id.
//
// A retry is REQUESTED/RETRY_WAIT before expiry and below the caps;
// otherwise the intent fails with EXPIRED_DEAD after expiry, or with
// ATTEMPTS_EXHAUSTED at a cap.
func retry(step Step, v Verdict, expired bool, limits Limits) Step {
	in := step.Before
	again := func(newBusiness bool) Step {
		switch {
		case expired:
			step.After = failed(step.After, ReasonExpiredDead, Code{})
		case newBusiness && in.Business >= limits.BusinessAttempts:
			step.After = failed(step.After, ReasonBusinessIDTaken, Code{})
		case in.Attempts >= limits.AttemptsPerIntent,
			!newBusiness && in.BusinessTries >= limits.AttemptsPerBusinessID:
			step.After = failed(step.After, ReasonAttemptsExhausted, Code{})
		default:
			step.After.State, step.After.Phase = StateRequested, PhaseRetryWait
			if newBusiness {
				step.After.Business++
				step.After.BusinessTries = 0
				step.NewBusinessAttempt = true
			}
		}
		return step
	}
	fail := func(r Reason, c Code) Step {
		step.After = failed(step.After, r, c)
		return step
	}
	switch v.attempt.Phase {
	case AttemptExecutionFailed:
		if v.code == CodeSVNReplay {
			return again(true)
		}
		return fail(ReasonExecutionFailed, v.code)
	case AttemptIncludedAnteFailed:
		switch v.code {
		case CodeSDKTimeoutHeight, CodeSDKTimeout, CodeSDKWrongSequence, CodeSDKOutOfGas:
			return again(false)
		}
		return fail(ReasonExecutionFailed, v.code)
	}
	if v.attempt.Dead != DeadRefused {
		return again(false)
	}
	last := v.attempt.Sends.Last
	switch {
	case last.Outcome == SendNodeRefused:
		return again(false)
	case last.Code == CodeSDKUnauthorized:
		step.After.Sdk4Deaths++
		if step.After.Sdk4Deaths > limits.Sdk4Retries {
			step.Alert = AlertSdk4Exhausted
			return fail(ReasonRejected, last.Code)
		}
		step = again(false)
		if step.After.State == StateRequested {
			step.WaitBlocks = Sdk4RetryGapBlocks
		}
		return step
	case last.Code == CodeSVNUnauthorized:
		step.Halt = HaltUnauthorized
	}
	return fail(ReasonRejected, last.Code)
}
