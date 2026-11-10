// Package policy is the publication and repair selection rules.
// A selected host is a proposal. It is not possession and it is not a
// durability guarantee. Declared operator and failure-domain strings are
// claims. This package does not sign.
package policy

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// ReplicaTarget is the proposed beta publication parameter.
	ReplicaTarget = 3

	RoleReplica      = "replica"
	RoleSpare        = "spare"
	RoleOriginBackup = "origin_backup"

	PublicationPublished = "PUBLISHED"
	PublicationWithheld  = "WITHHELD"

	EvidenceNone               = "NONE"
	EvidenceProviderAcceptance = "PROVIDER_ACCEPTANCE"
	EvidenceStale              = "STALE"
	EvidenceDishonest          = "DISHONEST"
	ClaimLimit                 = "ATTESTATION_NOT_STORAGE_PROOF"

	Reachable = "REACHABLE"
	Degraded  = "REACHABILITY_DEGRADED"

	RenewalNone    = "NONE"
	RenewalNeeded  = "RENEWAL_NEEDED"
	RenewalExpired = "EXPIRED"

	JobRunning         = "RUNNING"
	JobConfirmed       = "CONFIRMED"
	JobUnrecoverable   = "UNRECOVERABLE"
	JobWithdrawn       = "STOPPED_WITHDRAWN"
	JobCapacity        = "CAPACITY"
	JobCooling         = "COOLING"
	JobExhausted       = "EXHAUSTED"
	JobDuplicate       = "DUPLICATE"
	ServiceReadModel   = "READ_MODEL"
	ServiceTemporary   = "TEMPORARY_OFF_CHAIN"
	QueuedNotCommitted = "QUEUED_NOT_COMMITTED"
)

// Host is one admitted record plus a fresh reachability observation.
type Host struct {
	ProviderID       string
	DeclaredOperator string
	DeclaredDomain   string
	Simulated        bool
	PolicyID         string
	Capacity         uint64
	Used             uint64
	Reachable        bool
	Admitted         bool
	Draining         bool
	Role             string
	Holding          bool
}

// Skip is a host the selector did not propose, and why.
type Skip struct {
	ProviderID string
	Reason     string
}

// Decision is a proposal. Chosen is not a storage proof.
type Decision struct {
	Chosen  []Host
	Skipped []Skip
}

// Select proposes up to need hosts. Origin backups are never proposals.
// A spare is proposed only when allowSpare is set. Operator identity is the
// declared label, so two labels are not two physical machines.
func Select(hosts []Host, bytes uint64, policyID string, allowSpare bool, need int) Decision {
	ordered := append([]Host(nil), hosts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ProviderID < ordered[j].ProviderID })
	var out Decision
	seen := map[string]bool{}
	for _, host := range ordered {
		reason := reject(host, bytes, policyID, allowSpare, seen)
		if reason != "" {
			out.Skipped = append(out.Skipped, Skip{ProviderID: host.ProviderID, Reason: reason})
			continue
		}
		if len(out.Chosen) == need {
			out.Skipped = append(out.Skipped, Skip{ProviderID: host.ProviderID, Reason: "not_needed"})
			continue
		}
		seen[host.DeclaredOperator] = true
		out.Chosen = append(out.Chosen, host)
	}
	return out
}

func reject(host Host, bytes uint64, policyID string, allowSpare bool, seen map[string]bool) string {
	switch {
	case !host.Admitted:
		return "not_admitted"
	case host.Draining:
		return "draining"
	case host.Role == RoleOriginBackup:
		return "origin_backup"
	case host.Role == RoleSpare && !allowSpare:
		return "spare_held_back"
	case host.Role != RoleReplica && host.Role != RoleSpare:
		return "role"
	case host.PolicyID != policyID:
		return "policy"
	case host.Holding:
		return "holding"
	case host.Capacity < bytes || host.Used > host.Capacity-bytes:
		return "capacity"
	case !host.Reachable:
		return "unreachable"
	case host.DeclaredOperator == "" || seen[host.DeclaredOperator]:
		return "operator"
	default:
		return ""
	}
}

// Gate is the publication rule: community approval, verified transfers, and
// the target number of active operator-distinct commitments. Any miss
// withholds publication.
func Gate(approved bool, verified, distinct, target int) (string, string) {
	if !approved {
		return PublicationWithheld, "community_approval"
	}
	if verified < target {
		return PublicationWithheld, "verified_transfer"
	}
	if distinct < target {
		return PublicationWithheld, "operator_distinct_commitments"
	}
	return PublicationPublished, ""
}

// Evidence classifies one observation. A matching receipt is an attestation.
// It is not a storage proof. A mismatched receipt is dishonest. An old
// observation is stale and does not clear a degraded reachability mark.
func Evidence(measured, claimed string, age, maxAge time.Duration) (string, string) {
	if claimed != "" && measured != "" && !strings.EqualFold(claimed, measured) {
		return EvidenceDishonest, ClaimLimit
	}
	if maxAge > 0 && age > maxAge {
		return EvidenceStale, ClaimLimit
	}
	if measured != "" && strings.EqualFold(claimed, measured) {
		return EvidenceProviderAcceptance, ClaimLimit
	}
	return EvidenceNone, ""
}

// Renewal is a notice. It does not extend a lease.
func Renewal(deadline, now, notice int64) string {
	if now >= deadline {
		return RenewalExpired
	}
	if notice > 0 && deadline-now <= notice {
		return RenewalNeeded
	}
	return RenewalNone
}

// MayDepart is false while the other reachable verified copies are below
// the publication target. The caller must not delete that copy yet.
func MayDepart(otherReachable, target int) bool {
	return otherReachable >= target
}

// Job is one repair attempt. Key is stable for a content version.
type Job struct {
	Key         string
	State       string
	Attempts    int
	NextUnix    int64
	Replacement string
	Source      string
}

// TouchJob starts a job or returns the existing one. A running job and a
// job still inside its cooldown do not start another attempt.
func TouchJob(prev *Job, now, cooldown int64, max int) (Job, bool) {
	if prev == nil {
		return Job{State: JobRunning, Attempts: 1}, true
	}
	job := *prev
	switch job.State {
	case JobRunning:
		return job, false
	case JobConfirmed, JobWithdrawn, JobExhausted:
		return job, false
	case JobCooling, JobUnrecoverable, JobCapacity:
		if now < job.NextUnix {
			return job, false
		}
		if job.Attempts >= max {
			job.State = JobExhausted
			return job, false
		}
		job.Attempts++
		job.State = JobRunning
		job.NextUnix = 0
		return job, true
	default:
		return job, false
	}
}

// Segment is one package piece. ByteCount is the decimal byte length.
type Segment struct {
	ID        string
	SHA256    string
	ByteCount uint64
}

// ManifestText is the SVN-JCS-1 document the provider recomputes.
// The manifest id is "m" plus the content id, matching the provider.
func ManifestText(contentID string, version, packageBytes uint64, segs []Segment) string {
	var b strings.Builder
	b.WriteString(`{"content_id":`)
	writeString(&b, contentID)
	b.WriteString(`,"content_version":`)
	b.WriteString(strconv.FormatUint(version, 10))
	b.WriteString(`,"manifest_id":`)
	writeString(&b, "m"+contentID)
	b.WriteString(`,"package_bytes":`)
	writeString(&b, strconv.FormatUint(packageBytes, 10))
	b.WriteString(`,"profile":"SVN-JCS-1","renditions":[{"codec":"raw","rendition_id":"r0","segments":[`)
	for i, seg := range segs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"byte_count":`)
		writeString(&b, strconv.FormatUint(seg.ByteCount, 10))
		b.WriteString(`,"segment_id":`)
		writeString(&b, seg.ID)
		b.WriteString(`,"sha256":`)
		writeString(&b, seg.SHA256)
		b.WriteByte('}')
	}
	b.WriteString(`]}],"schema_version":1}`)
	return b.String()
}

func writeString(b *strings.Builder, text string) {
	b.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
