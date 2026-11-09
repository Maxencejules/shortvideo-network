package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// MaxGovernancePages bounds the Query/Governance pages a start-up check
// reads. Each page holds up to genesis.MaxPageLimit items of the walk
// (validators, submitters, seats, pending changes, proposals); the devnet
// has a few dozen. A chain whose walk is longer is refused, not truncated.
const MaxGovernancePages = 64

// GovernanceReader is the Query/Governance read (*chain.Client).
type GovernanceReader interface {
	Governance(ctx context.Context, pageKey []byte, limit uint32, minHeight int64) (*authorityv1.QueryGovernanceResponse, int64, error)
}

// GovernanceView is the role of every submitter and the address of every
// council seat, read at one height or later.
type GovernanceView struct {
	Height int64
	roles  map[string]typesv1.SubmitterRole
	seats  map[string]string
}

// RoleOf returns the submitter role of a 20-byte address, or
// SUBMITTER_ROLE_UNSPECIFIED when the address has none.
func (v GovernanceView) RoleOf(addr []byte) typesv1.SubmitterRole {
	return v.roles[string(addr)]
}

// SeatOf returns the seat id when addr is a council seat address.
func (v GovernanceView) SeatOf(addr []byte) (string, bool) {
	id, ok := v.seats[string(addr)]
	return id, ok
}

// RoleView is the role lookup the forward check uses (GovernanceView).
type RoleView interface {
	RoleOf(addr []byte) typesv1.SubmitterRole
}

// ReadGovernance walks Query/Governance from the first page, at least at
// minHeight, every later page at least at the first page's height, and at
// most MaxGovernancePages pages. A seat's address is derived from its
// ed25519 public key as the chain derives it (genesis.SeatAddress).
func ReadGovernance(ctx context.Context, r GovernanceReader, minHeight int64) (GovernanceView, error) {
	view := GovernanceView{roles: map[string]typesv1.SubmitterRole{}, seats: map[string]string{}}
	var pageKey []byte
	for page := 0; ; page++ {
		if page == MaxGovernancePages {
			return GovernanceView{}, fmt.Errorf("policy: Query/Governance has more than %d pages", MaxGovernancePages)
		}
		resp, height, err := r.Governance(ctx, pageKey, genesis.MaxPageLimit, minHeight)
		if err != nil {
			return GovernanceView{}, fmt.Errorf("policy: read governance: %w", err)
		}
		if page == 0 {
			view.Height, minHeight = height, height
		}
		state := resp.GetState()
		for _, s := range state.GetSubmitters() {
			if len(s.GetAddress()) != genesis.AccountBytes {
				return GovernanceView{}, errors.New("policy: governance submitter without a 20-byte address")
			}
			if _, dup := view.roles[string(s.GetAddress())]; dup {
				return GovernanceView{}, errors.New("policy: governance lists one submitter address twice")
			}
			view.roles[string(s.GetAddress())] = s.GetRole()
		}
		for _, seat := range append(append([]*authorityv1.DevnetTestSeat(nil), state.GetDevnetTestSeats()...), state.GetApprovedCouncil()...) {
			if len(seat.GetPublicKey()) != 32 {
				return GovernanceView{}, errors.New("policy: governance seat without a 32-byte public key")
			}
			view.seats[string(genesis.SeatAddress(seat.GetPublicKey()))] = seat.GetSeatId()
		}
		next := resp.GetPage().GetNextPageKey()
		if len(next) == 0 {
			return view, nil
		}
		if bytes.Equal(next, pageKey) {
			return GovernanceView{}, errors.New("policy: Query/Governance returned the same page key twice")
		}
		pageKey = next
	}
}

// KeyIdentity is one configured broadcaster key as the role check sees it.
type KeyIdentity struct {
	Name    string
	Class   Class
	Address []byte
}

// CheckKeyRoles is the start-up refusal of plan 7.1: every key must have
// the role its class requires (ADMITTED_SERVICE for SERVICE, BROADCASTER
// for BROADCASTER). A key with any other role (AUTHORITY, AUDITOR,
// ADMITTED_PROVIDER), with no role, or at a council seat address is
// refused; so are an unknown class, two keys of one class (Q1: one key of
// each), two keys with one name, and two keys with one address.
func CheckKeyRoles(keys []KeyIdentity, view GovernanceView) error {
	if len(keys) == 0 {
		return refuse(CodeRole, "no key configured")
	}
	names, classes, addrs := map[string]bool{}, map[Class]bool{}, map[string]bool{}
	for _, k := range keys {
		want, ok := RequiredRole(k.Class)
		if !ok {
			return refuse(CodeRole, "key %q has unknown class %q", k.Name, k.Class)
		}
		if len(k.Address) != genesis.AccountBytes {
			return refuse(CodeRole, "key %q has no 20-byte address", k.Name)
		}
		if names[k.Name] || k.Name == "" {
			return refuse(CodeRole, "key name %q is empty or used twice", k.Name)
		}
		if classes[k.Class] {
			return refuse(CodeRole, "class %s is configured twice", k.Class)
		}
		if addrs[string(k.Address)] {
			return refuse(CodeRole, "key %q has the address of another configured key", k.Name)
		}
		names[k.Name], classes[k.Class], addrs[string(k.Address)] = true, true, true
		if seat, isSeat := view.SeatOf(k.Address); isSeat {
			return refuse(CodeRole, "key %q is council seat %q", k.Name, seat)
		}
		switch got := view.RoleOf(k.Address); {
		case got == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED:
			return refuse(CodeRole, "key %q has no on-chain role", k.Name)
		case got != want:
			return refuse(CodeRole, "key %q has role %s, class %s needs %s", k.Name, got, k.Class, want)
		}
	}
	return nil
}
