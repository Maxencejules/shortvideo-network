// Package project turns committed block events into projection effects.
//
// DecoderVersion is 1. An event whose type is svn.events.<version>.* with
// any version other than v1 is rejected. A v1 event that does not parse, and
// a known untyped coordination event with the wrong shape, are rejected.
// Neither case is applied, and neither is treated as an empty block.
//
// Events that are not svn.events and are not the known offer-closed event
// are ignored. A transaction whose code is not zero is ignored: a failed
// transaction did not commit the state the event describes.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
)

// DecoderVersion is the event decoder this binary applies.
const DecoderVersion = 1

// AcceptedVersion is the version segment of svn.events types this decoder accepts.
const AcceptedVersion = "v1"

const offerClosedType = "leases_offer_closed"

var (
	// ErrEventVersion is an svn.events type whose version is not v1.
	ErrEventVersion = errors.New("project: event version is not v1")
	// ErrMalformed is a v1 event, or a known coordination event, that does not decode.
	ErrMalformed = errors.New("project: malformed event")
)

// ZeroHash is the transaction hash used for a block event. It is 32 zero
// bytes, and the message index is -1, so it cannot collide with a transaction
// event.
var ZeroHash = bytes.Repeat([]byte{0}, 32)

// Source is the canonical reference of one committed event.
type Source struct {
	Height     int64
	TxHash     []byte
	MsgIndex   int32
	EventIndex int32
	Type       string
}

// Block is one committed block, already checked against its commit.
type Block struct {
	ChainID  string
	Height   int64
	Hash     []byte
	AppHash  []byte
	TimeUnix int64
	Txs      []Tx
	Finalize []abci.Event
}

// Tx is one transaction result in a committed block.
type Tx struct {
	Hash   []byte
	Code   uint32
	Events []abci.Event
}

// Effect is one projection change. Update kinds require the row to exist.
type Effect struct {
	Source   Source
	Provider *Provider
	Content  *Content
	Offer    *Offer
	Lease    *Lease
	Policy   *Policy
	Evidence *Evidence
}

// Provider is a provider registry row.
type Provider struct {
	ID            string
	State         string
	KeyGeneration int64
	SetGeneration bool
}

// Content is a content registry row. WithdrawOnly updates the tombstone
// flags and leaves the manifest in place. The row is not deleted.
type Content struct {
	ID            string
	Version       uint64
	Manifest      string
	PackageBytes  string
	Withdrawn     bool
	HistoryErased bool
	WithdrawOnly  bool
}

// Offer is an offer row. CloseOnly and AcceptOnly require the row.
type Offer struct {
	ID         string
	ProviderID string
	ContentID  string
	State      string
	CloseOnly  bool
	AcceptOnly bool
}

// Lease is a lease row. RenewOnly and EndOnly require the row. RenewOnly
// is the only way a term version changes, and it comes from a committed
// LeaseRenewed event.
type Lease struct {
	ID            string
	ProviderID    string
	ContentID     string
	Version       uint64
	PackageBytes  string
	State         string
	TermVersion   uint64
	Deadline      int64
	ReleasedBytes string
	Active        bool
	RenewOnly     bool
	EndOnly       bool
}

// Policy is the policy-version row. KeepKey leaves the stored parameter key.
type Policy struct {
	Version      uint64
	ParameterKey string
	KeepKey      bool
}

// Evidence is one accepted attestation record. It is not a storage proof
// and not a publication approval.
type Evidence struct {
	ID          string
	BatchID     string
	Kind        string
	Attestation bool
}

// Decode reads a committed block in order: BeginBlock events, successful
// transactions, EndBlock events. A version or decode error fails the block.
func Decode(block Block) ([]Effect, error) {
	var begin, end, txs []Effect
	for i, ev := range block.Finalize {
		src := Source{Height: block.Height, TxHash: ZeroHash, MsgIndex: -1, EventIndex: int32(i), Type: ev.Type}
		eff, mode, skip, err := decodeFinalize(ev, src)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		if mode == "EndBlock" {
			end = append(end, eff)
			continue
		}
		begin = append(begin, eff)
	}
	for _, tx := range block.Txs {
		if tx.Code != 0 {
			continue
		}
		for i, ev := range tx.Events {
			if !strings.HasPrefix(ev.Type, "svn.events.") {
				continue
			}
			if err := checkVersion(ev.Type); err != nil {
				return nil, err
			}
			msg, msgIndex, err := msgkit.ParseCommitted(ev)
			if err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrMalformed, ev.Type, err)
			}
			src := Source{
				Height: block.Height, TxHash: append([]byte(nil), tx.Hash...),
				MsgIndex: int32(msgIndex), EventIndex: int32(i), Type: ev.Type,
			}
			eff, err := fromMessage(msg, src)
			if err != nil {
				return nil, err
			}
			txs = append(txs, eff)
		}
	}
	out := make([]Effect, 0, len(begin)+len(txs)+len(end))
	out = append(out, begin...)
	out = append(out, txs...)
	out = append(out, end...)
	return out, nil
}

func decodeFinalize(ev abci.Event, src Source) (Effect, string, bool, error) {
	if ev.Type == offerClosedType {
		eff, mode, err := decodeOfferClosed(ev, src)
		return eff, mode, false, err
	}
	if !strings.HasPrefix(ev.Type, "svn.events.") {
		return Effect{}, "", true, nil
	}
	if err := checkVersion(ev.Type); err != nil {
		return Effect{}, "", false, err
	}
	msg, mode, err := msgkit.ParseBlockEvent(ev)
	if err != nil {
		return Effect{}, "", false, fmt.Errorf("%w: %s: %v", ErrMalformed, ev.Type, err)
	}
	eff, err := fromMessage(msg, src)
	if err != nil {
		return Effect{}, "", false, err
	}
	return eff, mode, false, nil
}

// checkVersion accepts only the v1 segment. svn.events.v10 is not v1.
func checkVersion(eventType string) error {
	const prefix = "svn.events."
	rest := strings.TrimPrefix(eventType, prefix)
	if rest == eventType {
		return fmt.Errorf("%w: %s", ErrMalformed, eventType)
	}
	version, name, ok := strings.Cut(rest, ".")
	if !ok || version == "" || name == "" || strings.Contains(name, ".") {
		return fmt.Errorf("%w: %s", ErrEventVersion, eventType)
	}
	if version != AcceptedVersion {
		return fmt.Errorf("%w: %s", ErrEventVersion, eventType)
	}
	return nil
}

func fromMessage(msg proto.Message, src Source) (Effect, error) {
	eff := Effect{Source: src}
	switch m := msg.(type) {
	case *eventsv1.ProviderRegistered:
		if !msgkit.ValidID(m.GetProviderId()) || m.GetState() == "" {
			return Effect{}, fmt.Errorf("%w: provider registered", ErrMalformed)
		}
		eff.Provider = &Provider{ID: m.GetProviderId(), State: m.GetState()}
	case *eventsv1.ProviderStateChanged:
		if !msgkit.ValidID(m.GetProviderId()) || m.GetState() == "" {
			return Effect{}, fmt.Errorf("%w: provider state", ErrMalformed)
		}
		eff.Provider = &Provider{
			ID: m.GetProviderId(), State: m.GetState(),
			KeyGeneration: int64(m.GetKeyGeneration()), SetGeneration: true,
		}
	case *eventsv1.ContentRegistered:
		if !msgkit.ValidID(m.GetContentId()) || m.GetContentVersion() < 1 || !canonicalHex(m.GetManifestSha256()) || !canonicalDec(m.GetPackageBytes()) {
			return Effect{}, fmt.Errorf("%w: content registered", ErrMalformed)
		}
		eff.Content = &Content{
			ID: m.GetContentId(), Version: m.GetContentVersion(),
			Manifest: m.GetManifestSha256(), PackageBytes: m.GetPackageBytes(),
		}
	case *eventsv1.ContentWithdrawn:
		if !msgkit.ValidID(m.GetContentId()) || m.GetContentVersion() < 1 {
			return Effect{}, fmt.Errorf("%w: content withdrawn", ErrMalformed)
		}
		eff.Content = &Content{
			ID: m.GetContentId(), Version: m.GetContentVersion(),
			Withdrawn: true, HistoryErased: m.GetHistoryErased(), WithdrawOnly: true,
		}
	case *eventsv1.LeaseOffered:
		if !msgkit.ValidID(m.GetOfferId()) || !msgkit.ValidID(m.GetProviderId()) || !msgkit.ValidID(m.GetContentId()) {
			return Effect{}, fmt.Errorf("%w: lease offered", ErrMalformed)
		}
		eff.Offer = &Offer{ID: m.GetOfferId(), ProviderID: m.GetProviderId(), ContentID: m.GetContentId(), State: "OPEN"}
	case *eventsv1.LeaseAccepted:
		if !msgkit.ValidID(m.GetLeaseId()) || !msgkit.ValidID(m.GetProviderId()) || !msgkit.ValidID(m.GetContentId()) || m.GetContentVersion() < 1 || !canonicalDec(m.GetPackageBytes()) {
			return Effect{}, fmt.Errorf("%w: lease accepted", ErrMalformed)
		}
		// The accept event does not name the offer. The offer stays OPEN until
		// leases_offer_closed or a reconciliation finding says otherwise.
		eff.Lease = &Lease{
			ID: m.GetLeaseId(), ProviderID: m.GetProviderId(), ContentID: m.GetContentId(),
			Version: m.GetContentVersion(), PackageBytes: m.GetPackageBytes(),
			State: "ACTIVE", TermVersion: 1, Deadline: m.GetDeadlineUnixSeconds(), Active: true,
		}
	case *eventsv1.LeaseRenewed:
		if !msgkit.ValidID(m.GetLeaseId()) || m.GetTermVersion() < 1 {
			return Effect{}, fmt.Errorf("%w: lease renewed", ErrMalformed)
		}
		eff.Lease = &Lease{
			ID: m.GetLeaseId(), TermVersion: m.GetTermVersion(),
			Deadline: m.GetDeadlineUnixSeconds(), State: "RENEWED", Active: true, RenewOnly: true,
		}
	case *eventsv1.LeaseEnded:
		if !msgkit.ValidID(m.GetLeaseId()) || (m.GetState() != "ENDED" && m.GetState() != "EXPIRED") || !canonicalDec(m.GetReleasedBytes()) {
			return Effect{}, fmt.Errorf("%w: lease ended", ErrMalformed)
		}
		eff.Lease = &Lease{
			ID: m.GetLeaseId(), State: m.GetState(), ReleasedBytes: m.GetReleasedBytes(), EndOnly: true,
		}
	case *eventsv1.PolicyChanged:
		if m.GetPolicyVersion() < 1 || m.GetParameterKey() == "" {
			return Effect{}, fmt.Errorf("%w: policy changed", ErrMalformed)
		}
		eff.Policy = &Policy{Version: m.GetPolicyVersion(), ParameterKey: m.GetParameterKey()}
	case *eventsv1.ProposalExecuted:
		if m.GetPolicyVersion() < 1 || !msgkit.ValidID(m.GetProposalId()) {
			return Effect{}, fmt.Errorf("%w: proposal executed", ErrMalformed)
		}
		eff.Policy = &Policy{Version: m.GetPolicyVersion(), KeepKey: true}
	case *eventsv1.EvidenceAccepted:
		if !msgkit.ValidID(m.GetEvidenceId()) || !msgkit.ValidID(m.GetBatchId()) || m.GetKind() == "" {
			return Effect{}, fmt.Errorf("%w: evidence accepted", ErrMalformed)
		}
		eff.Evidence = &Evidence{ID: m.GetEvidenceId(), BatchID: m.GetBatchId(), Kind: m.GetKind(), Attestation: m.GetAttestation()}
	default:
		return Effect{}, fmt.Errorf("%w: %s", ErrMalformed, src.Type)
	}
	return eff, nil
}

func decodeOfferClosed(ev abci.Event, src Source) (Effect, string, error) {
	if len(ev.Attributes) != 3 || ev.Attributes[0].Key != "offer_id" || ev.Attributes[1].Key != "reason" || ev.Attributes[2].Key != "mode" {
		return Effect{}, "", fmt.Errorf("%w: leases_offer_closed", ErrMalformed)
	}
	mode := ev.Attributes[2].Value
	if mode != "BeginBlock" && mode != "EndBlock" {
		return Effect{}, "", fmt.Errorf("%w: leases_offer_closed mode", ErrMalformed)
	}
	id, reason := ev.Attributes[0].Value, ev.Attributes[1].Value
	if !msgkit.ValidID(id) || (reason != "EXPIRED" && reason != "WITHDRAWN") {
		return Effect{}, "", fmt.Errorf("%w: leases_offer_closed values", ErrMalformed)
	}
	return Effect{Source: src, Offer: &Offer{ID: id, State: "CLOSED", CloseOnly: true}}, mode, nil
}

func canonicalHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || (c > '9' && c < 'a') || c > 'f' {
			return false
		}
	}
	return true
}

func canonicalDec(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	if s[0] == '0' && len(s) > 1 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
