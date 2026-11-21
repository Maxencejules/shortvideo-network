package obs

import "fmt"

const (
	ObsDelivered        = "delivered_bytes"
	ObsStorage          = "storage_bytes"
	ObsCommitAge        = "commit_age_seconds"
	ObsQuorumLost       = "quorum_lost"
	ObsIndexLag         = "index_lag_heights"
	ObsIndexLagAge      = "index_lag_age_seconds"
	ObsProcessingAge    = "processing_age_seconds"
	ObsReplicas         = "reachable_replicas"
	ObsExpiry           = "expiry_backlog"
	ObsRepairBacklog    = "repair_backlog"
	ObsRepairFailure    = "repair_failure"
	ObsQueueRetries     = "queue_retries"
	ObsPlaybackFailure  = "playback_failures"
	ObsPlaybackStart    = "playback_starts"
	ObsPlaybackFallback = "playback_fallbacks"
	ObsReports          = "unreviewed_reports"
	ObsDiskFree         = "disk_free_bytes"
)

// Observation is one measurement. Account identifiers are not a field.
type Observation struct {
	Component     string
	CorrelationID string
	Kind          string
	Class         string
	Value         int64
	Message       string
}

// PlaybackSnapshot is an aggregate. It has no account and no watch event.
type PlaybackSnapshot struct {
	CorrelationID string `json:"correlation_id"`
	Class         string `json:"class"`
	Bytes         int64  `json:"bytes"`
	Failed        bool   `json:"failed"`
	Fallback      bool   `json:"fallback"`
}

// Observe records one measurement. Media kinds on the chain component are refused.
func (s *Service) Observe(o Observation) (string, error) {
	if !componentOK(o.Component) {
		return "", ErrComponent
	}
	if o.Component == CompChain && mediaKind(o.Kind) {
		return "", ErrOffChain
	}
	id := o.CorrelationID
	if !correlationOK(id) {
		var err error
		id, err = NewCorrelationID()
		if err != nil {
			return "", err
		}
	}
	o.CorrelationID = id
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.apply(o); err != nil {
		return "", err
	}
	s.traces[id] = append(s.traces[id], o.Component)
	s.appendLog(o.Component, id, fmt.Sprintf("kind=%s class=%s %s", o.Kind, o.Class, o.Message))
	return id, nil
}

func mediaKind(kind string) bool {
	switch kind {
	case ObsDelivered, ObsStorage, ObsPlaybackFailure, ObsPlaybackStart, ObsPlaybackFallback:
		return true
	default:
		return false
	}
}

func (s *Service) apply(o Observation) error {
	switch o.Kind {
	case ObsDelivered:
		if !classOK(o.Class) || o.Value < 1 {
			return ErrObservation
		}
		next, err := add(s.delivered[o.Class], o.Value)
		if err != nil {
			return err
		}
		s.delivered[o.Class] = next
		if o.Component == CompPlayback {
			s.playback.CorrelationID = o.CorrelationID
			s.playback.Class = o.Class
			s.playback.Bytes += o.Value
		}
	case ObsStorage:
		if !classOK(o.Class) || o.Value < 0 {
			return ErrObservation
		}
		s.storage[o.Class] = o.Value
	case ObsCommitAge:
		if o.Value < 0 {
			return ErrObservation
		}
		s.commitKnown = true
		s.commitAge = o.Value
		s.setGauge("commit_age_seconds", o.Value)
		s.refreshChain()
	case ObsQuorumLost:
		if o.Value != 0 && o.Value != 1 {
			return ErrObservation
		}
		s.quorumKnown = true
		s.quorumLost = o.Value == 1
		s.refreshChain()
	case ObsIndexLag:
		if o.Value < 0 {
			return ErrObservation
		}
		s.indexLag = o.Value
		s.setGauge("index_lag_heights", o.Value)
		s.alerts["index_lag"] = o.Value > 0
	case ObsIndexLagAge:
		if o.Value < 0 {
			return ErrObservation
		}
		s.setGauge("index_lag_age_seconds", o.Value)
	case ObsProcessingAge:
		if o.Value < 0 {
			return ErrObservation
		}
		s.processingKnown = true
		s.processingAge = o.Value
		s.setGauge("processing_age_seconds", o.Value)
		s.alerts["processing_age"] = o.Value > s.processingLimit
	case ObsReplicas:
		if o.Value < 0 {
			return ErrObservation
		}
		s.setGauge("reachable_replicas", o.Value)
		s.alerts["replicas"] = o.Value == 0
	case ObsExpiry:
		if o.Value < 0 {
			return ErrObservation
		}
		s.setGauge("expiry_backlog", o.Value)
	case ObsRepairBacklog:
		if o.Value < 0 {
			return ErrObservation
		}
		s.repairBacklog = o.Value
		s.setGauge("repair_backlog", o.Value)
		s.alerts["repair_backlog"] = o.Value > 0
	case ObsRepairFailure:
		if o.Value < 1 {
			return ErrObservation
		}
		next, err := add(s.counters["repair_failures"], o.Value)
		if err != nil {
			return err
		}
		s.counters["repair_failures"] = next
		back, err := add(s.repairBacklog, o.Value)
		if err != nil {
			return err
		}
		s.repairBacklog = back
		s.setGauge("repair_backlog", back)
		s.alerts["repair_backlog"] = true
	case ObsQueueRetries:
		if o.Value < 1 {
			return ErrObservation
		}
		next, err := add(s.counters["queue_retries"], o.Value)
		if err != nil {
			return err
		}
		s.counters["queue_retries"] = next
	case ObsPlaybackFailure:
		if o.Value < 1 {
			return ErrObservation
		}
		next, err := add(s.counters["playback_failures"], o.Value)
		if err != nil {
			return err
		}
		s.counters["playback_failures"] = next
		s.alerts["playback_errors"] = true
		s.playback.Failed = true
	case ObsPlaybackStart:
		if o.Value < 1 {
			return ErrObservation
		}
		next, err := add(s.counters["playback_starts"], o.Value)
		if err != nil {
			return err
		}
		s.counters["playback_starts"] = next
	case ObsPlaybackFallback:
		if o.Value < 1 {
			return ErrObservation
		}
		next, err := add(s.counters["playback_fallbacks"], o.Value)
		if err != nil {
			return err
		}
		s.counters["playback_fallbacks"] = next
		s.playback.Fallback = true
	case ObsReports:
		if o.Value < 0 {
			return ErrObservation
		}
		s.setGauge("unreviewed_reports", o.Value)
		s.alerts["unreviewed_reports"] = o.Value > 0
	case ObsDiskFree:
		if o.Value < 0 {
			return ErrObservation
		}
		s.diskKnown = true
		s.diskFree = o.Value
		s.setGauge("disk_free_bytes", o.Value)
		s.alerts["low_disk"] = o.Value < s.diskFloor
	default:
		return ErrObservation
	}
	return nil
}

func (s *Service) setGauge(name string, value int64) {
	s.gauges[name] = value
	s.gaugeSet[name] = true
}

func add(cur, delta int64) (int64, error) {
	if delta < 0 {
		return 0, ErrUnits
	}
	if cur > int64(^uint64(0)>>1)-delta {
		return 0, ErrUnits
	}
	return cur + delta, nil
}

// Playback returns the aggregate. The struct has no account field.
func (s *Service) Playback() PlaybackSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.playback
	if correlationOK(out.CorrelationID) {
		return out
	}
	out.CorrelationID = ""
	return out
}

// NoteRepairFailure counts a failed repair and does not add delivered bytes.
func (s *Service) NoteRepairFailure(component, correlationID string) (string, error) {
	if component == "" {
		component = CompProviders
	}
	return s.Observe(Observation{
		Component: component, CorrelationID: correlationID,
		Kind: ObsRepairFailure, Value: 1, Message: "repair failed",
	})
}

// Known fixture bytes. These are test inputs, not a measurement of a machine
// and not the blueprint's planning arithmetic.
const (
	FixtureUserPayload        int64 = 1000
	FixtureReplication        int64 = 3000
	FixtureRepair             int64 = 500
	FixturePrefetch           int64 = 200
	FixtureBackup             int64 = 5000
	FixtureStorageUser        int64 = 1000
	FixtureStorageReplication int64 = 3000
	FixtureStorageBackup      int64 = 5000
)

// ApplyKnownFixture records the fixture once per class.
func (s *Service) ApplyKnownFixture() error {
	rows := []Observation{
		{Component: CompPlayback, Kind: ObsDelivered, Class: ClassUserPayload, Value: FixtureUserPayload},
		{Component: CompProviders, Kind: ObsDelivered, Class: ClassReplication, Value: FixtureReplication},
		{Component: CompProviders, Kind: ObsDelivered, Class: ClassRepair, Value: FixtureRepair},
		{Component: CompPlayback, Kind: ObsDelivered, Class: ClassPrefetch, Value: FixturePrefetch},
		{Component: CompProviders, Kind: ObsDelivered, Class: ClassBackup, Value: FixtureBackup},
		{Component: CompPlayback, Kind: ObsStorage, Class: ClassUserPayload, Value: FixtureStorageUser},
		{Component: CompProviders, Kind: ObsStorage, Class: ClassReplication, Value: FixtureStorageReplication},
		{Component: CompProviders, Kind: ObsStorage, Class: ClassBackup, Value: FixtureStorageBackup},
	}
	for _, row := range rows {
		if _, err := s.Observe(row); err != nil {
			return err
		}
	}
	return nil
}

// Metrics is a copy of the counters. Unset gauges are omitted.
type Metrics struct {
	Gauges             map[string]int64 `json:"gauges"`
	Counters           map[string]int64 `json:"counters"`
	DeliveredBytes     map[string]int64 `json:"delivered_bytes"`
	StorageBytes       map[string]int64 `json:"storage_bytes"`
	Unmeasured         []string         `json:"unmeasured"`
	ChainSubmissions   int              `json:"chain_submissions"`
	WatchEventsOnChain int              `json:"watch_events_on_chain"`
	PublicLaunch       bool             `json:"public_launch"`
}

// Snapshot copies the measurements.
func (s *Service) Snapshot() Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Metrics{
		Gauges:             copySet(s.gauges, s.gaugeSet),
		Counters:           copyMap(s.counters),
		DeliveredBytes:     copyMap(s.delivered),
		StorageBytes:       copyMap(s.storage),
		Unmeasured:         append([]string(nil), Unmeasured...),
		ChainSubmissions:   s.chainWrites,
		WatchEventsOnChain: 0,
		PublicLaunch:       false,
	}
}

func copyMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copySet(in map[string]int64, set map[string]bool) map[string]int64 {
	out := map[string]int64{}
	for k, on := range set {
		if on {
			out[k] = in[k]
		}
	}
	return out
}

func deliveredTotal(m map[string]int64) int64 {
	var n int64
	for _, v := range m {
		n += v
	}
	return n
}
