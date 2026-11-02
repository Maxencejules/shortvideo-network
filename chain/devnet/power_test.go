package devnet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmttypes "github.com/cometbft/cometbft/types"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// fixedNetwork is four prepared nodes and the account and seat keys, all
// derived from fixed secrets, so two genesis builds can be compared byte for
// byte. The accounts follow accountSpec (Init's order). It writes nothing.
type fixedNetwork struct {
	nodes    []prepared
	accounts []accountKey
	seats    []councilKey
}

func newFixedNetwork() fixedNetwork {
	nodes := make([]prepared, Validators)
	for i := range nodes {
		name := fmt.Sprintf("v%d", i+1)
		p2pAddr, rpcAddr, grpcAddr := listenAddresses(i, PeerLoopback)
		nodes[i] = prepared{
			name: name, home: "/fixed/nodes/" + name, moniker: name, peerID: "peer-" + name,
			p2p: p2pAddr, rpc: rpcAddr, grpc: grpcAddr,
			pub: cmted25519.GenPrivKeyFromSecret([]byte(name)).PubKey().Bytes(),
		}
	}
	seats := make([]councilKey, 4)
	for i := range seats {
		id := fmt.Sprintf("seat-%d", i+1)
		key := sdked.GenPrivKeyFromSecret([]byte(id))
		seats[i] = councilKey{id: id, key: key, pub: key.PubKey().Bytes()}
	}
	accounts := make([]accountKey, len(accountSpec))
	for i, spec := range accountSpec {
		accounts[i] = accountKey{name: spec.name, role: spec.role, key: sdksecp.GenPrivKeyFromSecret([]byte(spec.name))}
	}
	return fixedNetwork{
		nodes:    nodes,
		accounts: accounts,
		seats:    seats,
	}
}

func fixedOptions(power int64) Options {
	return Options{Home: "/fixed", ChainID: "shortvideo-dev-fixed", GenesisTime: time.Unix(1700000000, 0).UTC(), Peers: PeerLoopback, Power: power}
}

// build returns the genesis.json bytes Init would save and the topology
// nodes Init would publish, for a requested Options.Power.
func (n fixedNetwork) build(t *testing.T, requested int64) ([]byte, []byte, *cmttypes.GenesisDoc, genesis.Document) {
	t.Helper()
	power, err := validatorPower(requested)
	if err != nil {
		t.Fatal(err)
	}
	opts := fixedOptions(power)
	genDoc, doc, err := genesisDoc(opts, n.nodes, n.accounts, n.seats)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "genesis.json")
	if err := genDoc.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	public := make([]Node, len(n.nodes))
	for i := range n.nodes {
		public[i] = n.nodes[i].public(PeerLoopback, opts.Power)
	}
	topology, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	return raw, topology, genDoc, doc
}

// TestInitPowerDefaultIsOne starts no process. Options.Power 0 builds the
// same genesis bytes and topology nodes as Power 1, every validator has
// power 1, and Power 10 changes only the powers.
func TestInitPowerDefaultIsOne(t *testing.T) {
	chainaddress.Configure()
	network := newFixedNetwork()
	zeroGenesis, zeroTopology, zeroDoc, zeroSource := network.build(t, 0)
	oneGenesis, oneTopology, _, _ := network.build(t, 1)
	if !bytes.Equal(zeroGenesis, oneGenesis) || !bytes.Equal(zeroTopology, oneTopology) {
		t.Fatal("Power 0 and Power 1 build different bytes")
	}
	tenGenesis, _, tenDoc, tenSource := network.build(t, 10)
	if bytes.Equal(zeroGenesis, tenGenesis) {
		t.Fatal("Power 10 builds the default bytes")
	}
	for _, tc := range []struct {
		power  int64
		comet  *cmttypes.GenesisDoc
		source genesis.Document
	}{{1, zeroDoc, zeroSource}, {10, tenDoc, tenSource}} {
		if len(tc.comet.Validators) != Validators || len(tc.source.Validators) != Validators {
			t.Fatalf("power %d: %d comet and %d genesis validators", tc.power, len(tc.comet.Validators), len(tc.source.Validators))
		}
		for i := range tc.comet.Validators {
			if tc.comet.Validators[i].Power != tc.power || tc.source.Validators[i].Power != tc.power {
				t.Fatalf("power %d: validator %d has comet %d genesis %d", tc.power, i, tc.comet.Validators[i].Power, tc.source.Validators[i].Power)
			}
		}
	}
}

// TestInitPowerRejects starts no process. A negative power, or one whose
// four-validator total exceeds genesis.SafeIntegerMax, is an error before
// Init writes anything. The largest accepted power builds a genesis that
// app.AppStateFromDocument accepts, and one more is the value genesis
// validation itself rejects.
func TestInitPowerRejects(t *testing.T) {
	chainaddress.Configure()
	largest := genesis.SafeIntegerMax / Validators
	for _, power := range []int64{-1, math.MinInt64, largest + 1, math.MaxInt64} {
		if _, err := validatorPower(power); err == nil {
			t.Fatalf("power %d accepted", power)
		}
		home := filepath.Join(t.TempDir(), "home")
		if _, err := Init(Options{Home: home, Power: power}); err == nil {
			t.Fatalf("Init accepted power %d", power)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("Init with power %d left %s: %v", power, home, err)
		}
	}
	network := newFixedNetwork()
	if _, _, comet, _ := network.build(t, largest); comet.Validators[0].Power != largest {
		t.Fatalf("largest power built %d", comet.Validators[0].Power)
	}
	// The genesis rule agrees with validatorPower at the boundary.
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	over := document(fixedOptions(largest+1), network.nodes, network.accounts, network.seats)
	if _, err := app.AppStateFromDocument(enc.Codec, over); err == nil {
		t.Fatal("genesis accepted a total above safe_integer_max")
	}
}

// TestInitPowerTopology starts no process. Init writes the requested power
// into the topology and into genesis.json, and Power 0 writes 1.
func TestInitPowerTopology(t *testing.T) {
	for _, tc := range []struct{ requested, want int64 }{{0, 1}, {10, 10}} {
		layout, err := Init(Options{Home: filepath.Join(t.TempDir(), "home"), Power: tc.requested})
		if err != nil {
			t.Fatal(err)
		}
		saved, err := cmttypes.GenesisDocFromFile(layout.Genesis)
		if err != nil {
			t.Fatal(err)
		}
		if len(layout.Topology.Validators) != Validators || len(saved.Validators) != Validators {
			t.Fatalf("requested %d: %d topology and %d genesis validators", tc.requested, len(layout.Topology.Validators), len(saved.Validators))
		}
		for i := range saved.Validators {
			if layout.Topology.Validators[i].Power != tc.want || saved.Validators[i].Power != tc.want {
				t.Fatalf("requested %d: validator %d topology %d genesis %d", tc.requested, i, layout.Topology.Validators[i].Power, saved.Validators[i].Power)
			}
		}
	}
}
