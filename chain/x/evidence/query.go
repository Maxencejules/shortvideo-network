package evidence

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Evidence is svn.evidence.v1.Query/Evidence (interim semantics, decision
// request DR-09-17). It reads committed state at the query height and never
// writes. A protocol failure is Code.QueryErr; a stored record or index row
// that breaks a store invariant is a plain error.
//
//   - evidence_id and lease_id both empty: MALFORMED (the protocol has no
//     unfiltered listing)
//   - a non-empty id outside the id pattern: BOUNDS (evidence_id first)
//   - page: msgkit.CheckPage against the effective max_page_limit (at most
//     100): a limit above the cap, a page_key above 64 bytes or outside the
//     id pattern BOUNDS; a nil page or limit 0 uses the cap
//   - evidence_id set (alone, or with lease_id): a non-empty page_key is
//     BOUNDS; the record when it exists (and, with lease_id, names that
//     lease), else an empty list (code 0); next_page_key empty
//   - lease_id only: the records of the lease in evidence id byte order;
//     page_key is the inclusive evidence id to start at; next_page_key is
//     the evidence id of the first record after the page, empty on the last
//     page
//
// Every returned record carries its REPORTED state (ReportedState at the
// query context's block time, deviation D-4): the SDK sets that time to the
// header time of the latest committed state, or, for a historical height, to
// that height's commit-info time when the node has it and otherwise to the
// latest header time. A context without a block time (a header time below
// 1, as the zero header of an App.Export context or of the check state
// right after a node restart carries) cannot report a state: a read that
// would return a record is a plain error (ErrNoBlockTime) instead of the
// stored state, so a long-expired RECORDED record never reads RECORDED. A
// read that returns no record needs no time. Every record is constant
// size, so a page holds at most cap records; the work is at most limit+1
// index steps and one record read per item.
func (k Keeper) Evidence(goCtx context.Context, req *evidencev1.QueryEvidenceRequest) (*evidencev1.QueryEvidenceResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	evidenceID, leaseID := req.GetEvidenceId(), req.GetLeaseId()
	if evidenceID == "" && leaseID == "" {
		return nil, msgkit.Malformed.QueryErr()
	}
	for _, id := range []string{evidenceID, leaseID} {
		if id != "" && !msgkit.ValidID(id) {
			return nil, msgkit.Bounds.QueryErr()
		}
	}
	limit, key, code := msgkit.CheckPage(req.GetPage(), k.pageLimit(ctx))
	if code != msgkit.OK {
		return nil, code.QueryErr()
	}
	now := ctx.BlockTime().Unix()
	out := &evidencev1.QueryEvidenceResponse{Page: &typesv1.PageResponse{}}
	if evidenceID != "" {
		if len(key) != 0 {
			return nil, msgkit.Bounds.QueryErr()
		}
		record, found, err := k.loadRecord(ctx, evidenceID)
		if err != nil {
			return nil, err
		}
		if found && (leaseID == "" || record.GetLeaseId() == leaseID) {
			report, err := reported(record, now)
			if err != nil {
				return nil, err
			}
			out.Records = append(out.Records, report)
		}
		return out, nil
	}
	prefix := leaseScope(leaseID)
	next, err := msgkit.Paginate(k.kv(ctx), prefix, key, limit, func(storeKey, value []byte) error {
		id := string(storeKey[len(prefix):])
		if !bytes.Equal(value, Marker) {
			return fmt.Errorf("evidence: lease index row %x holds %x", storeKey, value)
		}
		record, found, err := k.loadRecord(ctx, id)
		if err != nil {
			return err
		}
		if !found || record.GetLeaseId() != leaseID {
			return fmt.Errorf("evidence: lease index row %x names no record of lease %q", storeKey, leaseID)
		}
		report, err := reported(record, now)
		if err != nil {
			return err
		}
		out.Records = append(out.Records, report)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Page.NextPageKey = next
	return out, nil
}

// ErrNoBlockTime is the error of a query or an export that would report a
// record's state from a context whose block time is below 1 (no committed
// header time). It fails closed: the stored state is never reported in its
// place.
var ErrNoBlockTime = errors.New("evidence: the context has no committed block time; a record's reported state needs it")

// reported is a copy of record with its reported state at now. now below
// genesis.UnixSecondsMin (a context without a block time) is ErrNoBlockTime.
func reported(record *evidencev1.EvidenceRecord, now int64) (*evidencev1.EvidenceRecord, error) {
	if now < genesis.UnixSecondsMin {
		return nil, ErrNoBlockTime
	}
	out := proto.Clone(record).(*evidencev1.EvidenceRecord)
	out.State = ReportedState(record, now)
	return out, nil
}
