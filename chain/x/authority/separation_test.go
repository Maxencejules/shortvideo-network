package authority

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// TestSeatKeyIsNotAValidatorKeyD7: a council seat key and a consensus key
// are never the same key (D-7, DR-10-13), in both directions. Genesis
// refuses a seat key that is a genesis validator's key or a pending entry's
// key. An executed validator payload whose consensus key is a current or a
// retired seat key is DUPLICATE_ID (the propose is accepted: the check is
// the last one of the execute), leaving the store unchanged and the
// proposal QUEUED. A seat rotation onto a current or pending validator key
// is DUPLICATE_ID. A fresh key passes each path.
func TestSeatKeyIsNotAValidatorKeyD7(t *testing.T) {
	// Genesis.
	for _, r := range []struct {
		name string
		edit func(*authorityv1.GenesisState)
		want string
	}{
		{"seat key is a validator key", func(g *authorityv1.GenesisState) {
			g.Authority.DevnetTestSeats[1].PublicKey = pub(1)
		}, CodeDuplicate},
		{"seat key is a pending validator key", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g-add", "v5", pub(5), 1, 10)}
			g.Authority.DevnetTestSeats[2].PublicKey = pub(5)
		}, CodeDuplicate},
		{"pending entry with a fresh key", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g-add", "v5", pub(5), 1, 10)}
		}, ""},
	} {
		gen, _, _ := readyGenesis()
		r.edit(gen)
		err := ValidateGenesisState(gen, nil)
		if r.want == "" {
			if err != nil {
				t.Fatalf("%s: %v", r.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), r.want) {
			t.Fatalf("%s: %v, want %s", r.name, err, r.want)
		}
		kv := newMemKV()
		if InitStore(kv, gen) == nil || len(dumpKV(kv)) != 0 {
			t.Fatalf("%s: InitStore wrote a refused genesis", r.name)
		}
	}

	// Validator payloads at execute.
	kv, seats, authority := readyStore(t)
	execAt := func(name, id string, msg *authorityv1.MsgProposeChange, want string) {
		t.Helper()
		queueAt(t, kv, seats, testHeight, testTime, id, msg)
		before := dumpKV(kv)
		got := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-"+id, id))
		if got.Code != want {
			t.Fatalf("%s: %+v, want %q", name, got, want)
		}
		if want != "" {
			wantUnchanged(t, kv, before, name)
			if recordOf(t, kv, id).state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				t.Fatalf("%s: proposal left QUEUED", name)
			}
		}
	}
	execAt("new validator with a current seat key", "d7-seat", powerMsg("v5", seats[0].Pub, 1, 50), CodeDuplicate)
	execAt("key change onto a current seat key", "d7-seat-v2", powerMsg("v2", seats[1].Pub, 1, 50), CodeDuplicate)
	old := seats[3].Pub
	st := loadAt(t, kv, testHeight, testTime)
	if out := st.RotateCouncilSeat(seats[3].ID, pub(70)); !out.Success() {
		t.Fatal(out)
	}
	seats[3] = testSeat{ID: seats[3].ID, Pub: pub(70), Signer: SeatAddress(pub(70))}
	execAt("new validator with a retired seat key", "d7-retired", powerMsg("v5", old, 1, 50), CodeDuplicate)
	execAt("new validator with the rotated seat key", "d7-rotated", powerMsg("v5", pub(70), 1, 50), CodeDuplicate)
	execAt("new validator with a fresh key", "d7-fresh", powerMsg("v5", pub(5), 1, 50), "")

	// Seat rotations onto consensus keys. v5 (pub(5)) is pending now.
	for _, r := range []struct {
		name string
		key  []byte
		want string
	}{
		{"current validator key", pub(2), CodeDuplicate},
		{"pending validator key", pub(5), CodeDuplicate},
		{"fresh key", pub(71), ""},
	} {
		before := dumpKV(kv)
		got := loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[0].ID, r.key)
		if got.Code != r.want {
			t.Fatalf("rotation onto a %s: %+v, want %q", r.name, got, r.want)
		}
		if r.want != "" {
			wantUnchanged(t, kv, before, "rotation onto a "+r.name)
		}
	}
}

// TestSeatIDLengthAtGenesisD10: genesis accepts a seat id of at most 59
// characters (D-10, DR-10-42) and refuses 60 and 64 with BOUNDS, although
// the id pattern allows 64. "seat-" plus a 59-character id is the longest
// string the 64-character id pattern accepts.
func TestSeatIDLengthAtGenesisD10(t *testing.T) {
	id59 := strings.Repeat("s", MaxSeatIDLength)
	if !idPattern.MatchString("seat-"+id59) || idPattern.MatchString("seat-"+id59+"s") {
		t.Fatal("the cap does not match the id pattern")
	}
	for _, r := range []struct {
		length int
		want   string
	}{{MaxSeatIDLength, ""}, {MaxSeatIDLength + 1, CodeBounds}, {64, CodeBounds}} {
		gen, _, _ := readyGenesis()
		gen = proto.Clone(gen).(*authorityv1.GenesisState)
		gen.Authority.DevnetTestSeats[3].SeatId = strings.Repeat("s", r.length)
		if !idPattern.MatchString(gen.Authority.DevnetTestSeats[3].SeatId) {
			t.Fatalf("length %d is outside the id pattern", r.length)
		}
		err := ValidateGenesisState(gen, nil)
		if r.want == "" {
			if err != nil {
				t.Fatalf("length %d: %v", r.length, err)
			}
			kv := storeFrom(t, gen)
			st := loadAt(t, kv, testHeight, testTime)
			if st.seatOf(SeatAddress(gen.Authority.DevnetTestSeats[3].GetPublicKey())).GetSeatId() != gen.Authority.DevnetTestSeats[3].SeatId {
				t.Fatalf("length %d: the seat is not stored", r.length)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), r.want) {
			t.Fatalf("length %d: %v, want %s", r.length, err, r.want)
		}
	}
}
