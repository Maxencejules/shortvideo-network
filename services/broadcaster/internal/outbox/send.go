package outbox

import (
	"errors"
	"fmt"
)

// Send is one recorded send outcome (plan 8.4, first table).
type Send struct {
	Outcome SendOutcome
	// Code is the CheckTx code of a SendMempoolRejected outcome and zero
	// for every other outcome.
	Code Code
}

func (s Send) validate() error {
	if s.Outcome < SendUnknown || s.Outcome > SendAck {
		return fmt.Errorf("outbox: send outcome %d unknown", s.Outcome)
	}
	if err := s.Code.validate(); err != nil {
		return err
	}
	if (s.Outcome == SendMempoolRejected) == s.Code.OK() {
		return errors.New("outbox: a CheckTx rejection needs a failure code and no other outcome has one")
	}
	return nil
}

// Refusal reports whether s counts as a definitive refusal for the dead
// rule (d2): a node refusal, or a CheckTx rejection with any code but sdk
// 32. A refused transaction never entered the node's mempool, so it is not
// gossiped. Sdk 32 does not count: after a node restart empties its cache,
// the bytes of a committed transaction get sdk 32. UNKNOWN, CACHE and ACK
// never count.
func (s Send) Refusal() bool {
	switch s.Outcome {
	case SendNodeRefused:
		return true
	case SendMempoolRejected:
		return s.Code != CodeSDKWrongSequence
	}
	return false
}

// Sends are one attempt's send counters, as the outbox schema stores them.
// A send is counted in Started before it happens (plan 8.3 step 2) and in
// Recorded once its outcome is stored (step 4).
type Sends struct {
	Started  int
	Recorded int
	Unknown  int
	Cache    int
	Refusals int
	Acked    bool
	// Last is the last recorded outcome; zero before the first record.
	Last Send
}

func (s Sends) validate() error {
	for _, v := range []int{s.Started, s.Recorded, s.Unknown, s.Cache, s.Refusals} {
		if v < 0 || v > MaxSends {
			return fmt.Errorf("outbox: send counter %d outside [0, %d]", v, MaxSends)
		}
	}
	if s.Recorded > s.Started {
		return errors.New("outbox: more sends recorded than started")
	}
	if s.Unknown+s.Cache+s.Refusals > s.Recorded {
		return errors.New("outbox: classified sends exceed recorded sends")
	}
	if (s.Recorded == 0) != (s.Last == Send{}) {
		return errors.New("outbox: the last outcome must be set exactly when a send is recorded")
	}
	if s.Recorded == 0 {
		if s.Acked {
			return errors.New("outbox: acknowledged without a recorded send")
		}
		return nil
	}
	if err := s.Last.validate(); err != nil {
		return err
	}
	if s.Last.Refusal() && s.Refusals == 0 {
		return errors.New("outbox: the last outcome is a refusal and no refusal is counted")
	}
	if s.Last.Outcome == SendAck && !s.Acked {
		return errors.New("outbox: the last outcome is an ACK and the attempt is not acknowledged")
	}
	return nil
}

// Unrecorded is the number of started sends whose outcome was never
// recorded. Each counts as UNKNOWN (plan 8.3): the bytes may be in a
// mempool.
func (s Sends) Unrecorded() int { return s.Started - s.Recorded }

// AllRefused reports the send condition of (d2): at least one send
// started, every started send recorded, and every recorded outcome a
// counted refusal. With zero started sends it is false (M1).
func (s Sends) AllRefused() bool {
	return s.Started >= 1 && s.Recorded == s.Started && s.Refusals == s.Recorded
}

// start counts one more started send.
func (s Sends) start() (Sends, error) {
	if s.Started >= MaxSends {
		return s, errors.New("outbox: send counter at its bound")
	}
	s.Started++
	return s, nil
}

// record stores the outcome of one started send.
func (s Sends) record(o Send) (Sends, error) {
	if err := o.validate(); err != nil {
		return s, err
	}
	if s.Recorded >= s.Started {
		return s, errors.New("outbox: an outcome needs a started send without a recorded outcome")
	}
	s.Recorded++
	switch {
	case o.Outcome == SendUnknown:
		s.Unknown++
	case o.Outcome == SendCache:
		s.Cache++
	case o.Outcome == SendAck:
		s.Acked = true
	case o.Refusal():
		s.Refusals++
	}
	s.Last = o
	return s, nil
}

// RaisePhase returns the phase of an attempt in phase p after a send
// outcome o (plan 9.2). Live phases only gain knowledge: ACK raises any live
// phase to ACKNOWLEDGED; UNKNOWN or CACHE raises SIGNED to UNCERTAIN and
// leaves UNCERTAIN and ACKNOWLEDGED as they are; a refusal or sdk 32 changes
// nothing. Terminal phases never change.
func RaisePhase(p AttemptPhase, o SendOutcome) AttemptPhase {
	if !p.Live() {
		return p
	}
	switch o {
	case SendAck:
		return AttemptAcknowledged
	case SendUnknown, SendCache:
		return max(p, AttemptUncertain)
	}
	return p
}
