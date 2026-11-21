package obs

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

const timeMinute = time.Minute

const (
	stateRunning  = "running"
	stateQueued   = "queued"
	stateReleased = "released"
)

// Admit applies a placeholder limit. A repeated idempotency key does not
// consume a second unit. Alerts are not consulted. SpendCap is false.
func (s *Service) Admit(kind string, units int64, key string) (Decision, error) {
	if units < 1 {
		return Decision{}, ErrUnits
	}
	if key != "" && !idemOK(key) {
		return Decision{}, ErrKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lim, ok := s.limits[kind]
	if !ok {
		return Decision{}, ErrKind
	}
	if lim.window {
		s.roll(lim)
		if key != "" {
			if prior, seen := lim.seen[key]; seen {
				prior.Replayed = true
				return prior, nil
			}
		}
	} else if key != "" {
		if id, seen := lim.idem[key]; seen {
			return s.replay(lim, id), nil
		}
	}
	if units > lim.limit {
		s.deny(kind)
		return decision(kind, "over_limit", "", false, false), nil
	}
	if reason := s.intakeReason(kind, units); reason != "" {
		s.deny(kind)
		s.alerts["admission"] = true
		return decision(kind, reason, "", false, false), nil
	}
	if lim.window {
		if lim.used+units > lim.limit {
			s.deny(kind)
			return decision(kind, "hard_stop", "", false, false), nil
		}
		lim.used += units
		d := decision(kind, "admitted", "", true, false)
		if key != "" {
			lim.seen[key] = d
		}
		return d, nil
	}
	if lim.used+units <= lim.limit {
		return s.hold(lim, kind, units, key, stateRunning), nil
	}
	if lim.queue > 0 && lim.queued+units <= lim.queue {
		return s.hold(lim, kind, units, key, stateQueued), nil
	}
	s.deny(kind)
	return decision(kind, "hard_stop", "", false, false), nil
}

// Release returns capacity. A second release of the same grant does not
// free the units again.
func (s *Service) Release(id string) (bool, bool, error) {
	if id == "" {
		return false, false, ErrGrant
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, lim := range s.limits {
		g, ok := lim.grants[id]
		if !ok {
			continue
		}
		if g.state == stateReleased {
			return true, true, nil
		}
		if g.state == stateRunning {
			lim.used -= g.units
			g.state = stateReleased
			s.promote(lim)
			return true, false, nil
		}
		lim.queued -= g.units
		lim.order = dropID(lim.order, id)
		g.state = stateReleased
		return true, false, nil
	}
	return false, false, ErrGrant
}

func (s *Service) roll(lim *limiter) {
	if s.now().Sub(lim.start) >= timeMinute {
		lim.used = 0
		lim.start = s.now()
		lim.seen = map[string]Decision{}
	}
}

func (s *Service) replay(lim *limiter, id string) Decision {
	g := lim.grants[id]
	d := decision(g.kind, "admitted", id, true, false)
	d.Replayed = true
	switch g.state {
	case stateQueued:
		d.Granted = false
		d.Queued = true
		d.Reason = "queued"
	case stateReleased:
		d.Granted = false
		d.Reason = "already_released"
	}
	return d
}

func (s *Service) hold(lim *limiter, kind string, units int64, key, state string) Decision {
	id := newGrantID()
	g := &grant{id: id, kind: kind, units: units, state: state}
	lim.grants[id] = g
	if key != "" {
		lim.idem[key] = id
	}
	granted := state == stateRunning
	queued := state == stateQueued
	if granted {
		lim.used += units
	} else {
		lim.queued += units
		lim.order = append(lim.order, id)
	}
	reason := "admitted"
	if queued {
		reason = "queued"
	}
	return decision(kind, reason, id, granted, queued)
}

func (s *Service) promote(lim *limiter) {
	for len(lim.order) > 0 && lim.used < lim.limit {
		id := lim.order[0]
		g := lim.grants[id]
		if g.units > lim.limit-lim.used {
			return
		}
		lim.order = lim.order[1:]
		g.state = stateRunning
		lim.queued -= g.units
		lim.used += g.units
	}
}

func (s *Service) deny(kind string) {
	s.denials[kind]++
}

func (s *Service) intakeReason(kind string, units int64) string {
	switch kind {
	case KindProcessing:
		if s.processingKnown && s.processingAge > s.processingLimit {
			return "intake_stopped"
		}
	case KindStorage:
		if s.diskKnown && (s.diskFree < s.diskFloor || s.diskFree < units) {
			return "low_disk"
		}
		if s.repairBacklog > 0 {
			return "repair_backlog"
		}
	case KindAudit, KindRetries:
		if (s.quorumKnown && s.quorumLost) || (s.commitKnown && s.commitAge > s.commitLimit) {
			return "chain_unusable"
		}
	}
	return ""
}

func (s *Service) refreshChain() {
	s.deps["chain"].usable = s.commitKnown && s.quorumKnown && !s.quorumLost && s.commitAge <= s.commitLimit
	s.alerts["stale_commit"] = s.commitKnown && s.commitAge > s.commitLimit
	s.alerts["quorum_loss"] = s.quorumKnown && s.quorumLost
}

func idemOK(key string) bool {
	if len(key) > 80 {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == ':' || c == '-':
		default:
			return false
		}
	}
	return true
}

func newGrantID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "g0000000000000000000000000000000"
	}
	return "g" + hex.EncodeToString(buf[:])
}

func dropID(ids []string, id string) []string {
	out := ids[:0]
	for _, item := range ids {
		if item != id {
			out = append(out, item)
		}
	}
	return out
}
