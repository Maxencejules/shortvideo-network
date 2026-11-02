// Package devnet builds a four-validator home and records its public topology.
//
// Keys are generated at runtime and written only under the home directory.
// The package never deletes a home once genesis.json exists.
package devnet

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	cmttypes "github.com/cometbft/cometbft/types"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/node"
)

const Validators = 4

// PeerMode selects how the four processes address each other.
type PeerMode string

const (
	PeerLoopback PeerMode = "loopback"
	PeerCompose  PeerMode = "compose"
)

// Options configures one disposable network. Home must not already exist.
type Options struct {
	Home        string
	ChainID     string
	GenesisTime time.Time
	Peers       PeerMode
	// Power is the voting power of every genesis validator. Zero means 1,
	// the default. All four validators get the same power because genesis
	// requires equal powers. The total, Validators*Power, must not exceed
	// genesis.SafeIntegerMax.
	Power int64
}

// validatorPower returns the power Init gives every genesis validator.
func validatorPower(requested int64) (int64, error) {
	if requested == 0 {
		return 1, nil
	}
	if requested < 0 {
		return 0, fmt.Errorf("validator power %d is not positive", requested)
	}
	if requested > genesis.SafeIntegerMax/Validators {
		return 0, fmt.Errorf("validator power %d: the total of %d validators exceeds safe_integer_max", requested, Validators)
	}
	return requested, nil
}

// Node is the public description of one validator. It has no private key.
type Node struct {
	Name               string `json:"name"`
	Home               string `json:"home"`
	Moniker            string `json:"moniker"`
	PeerID             string `json:"peer_id"`
	P2P                string `json:"p2p"`
	RPC                string `json:"rpc"`
	GRPC               string `json:"grpc"`
	Power              int64  `json:"power"`
	ConsensusPubkeyHex string `json:"consensus_pubkey_hex"`
}

// Topology is safe to print and to store in evidence.
type Topology struct {
	ChainID            string    `json:"chain_id"`
	GenesisUnixSeconds int64     `json:"genesis_unix_seconds"`
	GenesisSHA256      string    `json:"genesis_sha256"`
	PeerMode           string    `json:"peer_mode"`
	Validators         []Node    `json:"validators"`
	AuthorityAddress   string    `json:"authority_address"`
	ServiceAddress     string    `json:"service_address"`
	Seats              []Seat    `json:"seats,omitempty"`
	Accounts           []Account `json:"accounts,omitempty"`
}

// Account is one public application account of the devnet genesis. Its
// secp256k1 key is under accounts/<name>.hex in the home directory only.
type Account struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Address string `json:"address"`
}

// accountSpec lists the secp256k1 application accounts Init generates, in
// genesis order. authority and service are the Goal 05 accounts; the two
// providers and the second service account are the Goal 07 registry demo
// accounts.
var accountSpec = []struct {
	name string
	role string
}{
	{"authority", genesis.RoleAuthority},
	{"service", genesis.RoleAdmittedService},
	{"service-2", genesis.RoleAdmittedService},
	{"provider-1", genesis.RoleAdmittedProvider},
	{"provider-2", genesis.RoleAdmittedProvider},
}

// Seat is a public devnet council key. It is not a consensus key.
type Seat struct {
	SeatID       string `json:"seat_id"`
	Address      string `json:"address"`
	PublicKeyHex string `json:"public_key_hex"`
}

// Layout is the result of Init.
type Layout struct {
	Home     string
	Topology Topology
	Genesis  string
}

// Init creates four validator homes and one shared genesis.
// A second call for the same home fails. The function does not replace keys.
func Init(opts Options) (layout Layout, err error) {
	if opts.Home == "" {
		return Layout{}, fmt.Errorf("home is required")
	}
	if opts.Power, err = validatorPower(opts.Power); err != nil {
		return Layout{}, err
	}
	if opts.Peers == "" {
		opts.Peers = PeerLoopback
	}
	if opts.Peers != PeerLoopback && opts.Peers != PeerCompose {
		return Layout{}, fmt.Errorf("peer mode %q", opts.Peers)
	}
	if entries, readErr := os.ReadDir(opts.Home); readErr == nil && len(entries) > 0 {
		return Layout{}, fmt.Errorf("refusing to replace %s", opts.Home)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		return Layout{}, readErr
	}
	if err = os.MkdirAll(opts.Home, 0o700); err != nil {
		return Layout{}, err
	}
	defer func() {
		if err != nil {
			if _, statErr := os.Stat(filepath.Join(opts.Home, "genesis.json")); statErr != nil {
				_ = os.RemoveAll(opts.Home)
			}
		}
	}()

	chainaddress.Configure()
	if opts.ChainID == "" {
		var buf [8]byte
		if _, err = rand.Read(buf[:]); err != nil {
			return Layout{}, err
		}
		opts.ChainID = genesis.DevnetChainIDPrefix + hex.EncodeToString(buf[:])
	}
	if opts.GenesisTime.IsZero() {
		opts.GenesisTime = time.Now().UTC().Truncate(time.Second)
	}

	nodes := make([]prepared, Validators)
	for i := 0; i < Validators; i++ {
		nodes[i], err = prepareNode(opts.Home, i, opts.Peers)
		if err != nil {
			return Layout{}, err
		}
	}
	accounts := make([]accountKey, len(accountSpec))
	for i, spec := range accountSpec {
		key := sdksecp.GenPrivKey()
		accounts[i] = accountKey{name: spec.name, role: spec.role, key: key}
		if err = writeKey(filepath.Join(opts.Home, "accounts", spec.name+".hex"), key.Bytes()); err != nil {
			return Layout{}, err
		}
	}
	authorityKey, serviceKey := accounts[0].key, accounts[1].key
	seats := make([]councilKey, 4)
	for i := range seats {
		key := sdked.GenPrivKey()
		pub := key.PubKey().Bytes()
		if len(pub) != 32 {
			return Layout{}, fmt.Errorf("council public key length %d", len(pub))
		}
		seats[i] = councilKey{id: fmt.Sprintf("seat-%d", i+1), key: key, pub: pub}
		if err = writeKey(filepath.Join(opts.Home, "accounts", fmt.Sprintf("seat-%d.hex", i+1)), key.Bytes()); err != nil {
			return Layout{}, err
		}
	}

	genDoc, doc, err := genesisDoc(opts, nodes, accounts, seats)
	if err != nil {
		return Layout{}, err
	}
	genesisPath := filepath.Join(opts.Home, "genesis.json")
	if err = genDoc.SaveAs(genesisPath); err != nil {
		return Layout{}, err
	}
	genesisBytes, err := os.ReadFile(genesisPath)
	if err != nil {
		return Layout{}, err
	}
	sum := sha256.Sum256(genesisBytes)
	digest := hex.EncodeToString(sum[:])
	if err = os.WriteFile(filepath.Join(opts.Home, "genesis.sha256"), []byte(digest+"\n"), 0o644); err != nil {
		return Layout{}, err
	}
	for i := range nodes {
		dest := filepath.Join(nodes[i].home, "config", "genesis.json")
		if err = os.WriteFile(dest, genesisBytes, 0o644); err != nil {
			return Layout{}, err
		}
	}
	public := make([]Node, len(nodes))
	for i := range nodes {
		if err = writeNodeConfig(nodes[i], peersExcept(nodes, i, opts.Peers)); err != nil {
			return Layout{}, err
		}
		public[i] = nodes[i].public(opts.Peers, opts.Power)
	}
	topology := Topology{
		ChainID:            doc.ChainID,
		GenesisUnixSeconds: doc.GenesisUnixSeconds,
		GenesisSHA256:      digest,
		PeerMode:           string(opts.Peers),
		Validators:         public,
		AuthorityAddress:   sdk.AccAddress(authorityKey.PubKey().Address()).String(),
		ServiceAddress:     sdk.AccAddress(serviceKey.PubKey().Address()).String(),
		Seats:              publicSeats(seats),
		Accounts:           publicAccounts(accounts),
	}
	rawTopology, err := json.MarshalIndent(topology, "", "  ")
	if err != nil {
		return Layout{}, err
	}
	if err = os.WriteFile(filepath.Join(opts.Home, "topology.json"), append(rawTopology, '\n'), 0o644); err != nil {
		return Layout{}, err
	}
	return Layout{Home: opts.Home, Topology: topology, Genesis: genesisPath}, nil
}

// genesisDoc builds the validated CometBFT genesis for the prepared nodes
// and keys. It writes nothing. opts.Power must already be the value
// validatorPower returned.
func genesisDoc(opts Options, nodes []prepared, accounts []accountKey, seats []councilKey) (*cmttypes.GenesisDoc, genesis.Document, error) {
	doc := document(opts, nodes, accounts, seats)
	enc, err := app.MakeEncoding()
	if err != nil {
		return nil, genesis.Document{}, err
	}
	modules, err := app.AppStateFromDocument(enc.Codec, doc)
	if err != nil {
		return nil, genesis.Document{}, err
	}
	appState, err := json.Marshal(modules)
	if err != nil {
		return nil, genesis.Document{}, err
	}
	genDoc := &cmttypes.GenesisDoc{
		GenesisTime:     doc.Time(),
		ChainID:         doc.ChainID,
		InitialHeight:   1,
		ConsensusParams: consensusParams(),
		Validators:      cometValidators(nodes, opts.Power),
		AppState:        appState,
	}
	if err := genDoc.ValidateAndComplete(); err != nil {
		return nil, genesis.Document{}, err
	}
	return genDoc, doc, nil
}

type prepared struct {
	name    string
	home    string
	moniker string
	peerID  string
	p2p     string
	rpc     string
	grpc    string
	pub     []byte
}

func (p prepared) public(mode PeerMode, power int64) Node {
	return Node{
		Name:               p.name,
		Home:               p.home,
		Moniker:            p.moniker,
		PeerID:             p.peerID,
		P2P:                dialAddress(p, mode),
		RPC:                rpcURL(p),
		GRPC:               p.grpc,
		Power:              power,
		ConsensusPubkeyHex: hex.EncodeToString(p.pub),
	}
}

func prepareNode(root string, index int, mode PeerMode) (prepared, error) {
	name := fmt.Sprintf("v%d", index+1)
	home := filepath.Join(root, "nodes", name)
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot(home)
	cmtcfg.EnsureRoot(home)
	filePV := privval.GenFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile())
	filePV.Save()
	consensusPub, err := filePV.GetPubKey()
	if err != nil {
		return prepared{}, err
	}
	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.NodeKeyFile())
	if err != nil {
		return prepared{}, err
	}
	p2pAddr, rpcAddr, grpcAddr := listenAddresses(index, mode)
	return prepared{
		name:    name,
		home:    home,
		moniker: name,
		peerID:  string(nodeKey.ID()),
		p2p:     p2pAddr,
		rpc:     rpcAddr,
		grpc:    grpcAddr,
		pub:     append([]byte(nil), consensusPub.Bytes()...),
	}, nil
}

func listenAddresses(index int, mode PeerMode) (p2pAddr, rpcAddr, grpcAddr string) {
	if mode == PeerCompose {
		return "tcp://0.0.0.0:26656", "tcp://0.0.0.0:26657", fmt.Sprintf("0.0.0.0:%d", 27101+index)
	}
	return fmt.Sprintf("tcp://127.0.0.1:%d", 26656+index*100),
		fmt.Sprintf("tcp://127.0.0.1:%d", 26657+index*100),
		fmt.Sprintf("127.0.0.1:%d", 27101+index)
}

func dialAddress(p prepared, mode PeerMode) string {
	if mode == PeerCompose {
		return fmt.Sprintf("%s@%s:26656", p.peerID, p.name)
	}
	return fmt.Sprintf("%s@127.0.0.1:%d", p.peerID, 26656+indexOf(p.name)*100)
}

func indexOf(name string) int {
	var n int
	_, _ = fmt.Sscanf(name, "v%d", &n)
	return n - 1
}

func rpcURL(p prepared) string {
	// p.rpc is tcp://host:port. Status uses http.
	return "http://" + trimScheme(p.rpc)
}

func trimScheme(address string) string {
	for _, prefix := range []string{"tcp://", "http://"} {
		if len(address) > len(prefix) && address[:len(prefix)] == prefix {
			return address[len(prefix):]
		}
	}
	return address
}

func peersExcept(nodes []prepared, self int, mode PeerMode) string {
	parts := make([]string, 0, len(nodes)-1)
	for i := range nodes {
		if i == self {
			continue
		}
		parts = append(parts, dialAddress(nodes[i], mode))
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += "," + part
	}
	return out
}

func writeNodeConfig(p prepared, peers string) error {
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot(p.home)
	cfg.Moniker = p.moniker
	cfg.P2P.ListenAddress = p.p2p
	cfg.P2P.PersistentPeers = peers
	cfg.P2P.AllowDuplicateIP = true
	cfg.P2P.AddrBookStrict = false
	cfg.P2P.PexReactor = false
	cfg.P2P.Seeds = ""
	cfg.RPC.ListenAddress = p.rpc
	cfg.Instrumentation.Prometheus = false
	node.ApplyDevnetTimeouts(cfg)
	if err := cfg.ValidateBasic(); err != nil {
		return err
	}
	cmtcfg.WriteConfigFile(filepath.Join(p.home, "config", "config.toml"), cfg)
	saved := node.NodeFile{
		Moniker:          p.moniker,
		P2P:              p.p2p,
		RPC:              p.rpc,
		PersistentPeers:  peers,
		AllowDuplicateIP: true,
		AddrBookStrict:   false,
		Pex:              false,
	}
	raw, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.home, "config", "node.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	listen := node.Listen{GRPC: p.grpc, RPC: p.rpc, P2P: p.p2p}
	rawListen, err := json.MarshalIndent(listen, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.home, "config", "svn-listen.json"), append(rawListen, '\n'), 0o644)
}

type accountKey struct {
	name string
	role string
	key  *sdksecp.PrivKey
}

func publicAccounts(accounts []accountKey) []Account {
	out := make([]Account, len(accounts))
	for i, account := range accounts {
		out[i] = Account{
			Name:    account.name,
			Role:    account.role,
			Address: sdk.AccAddress(account.key.PubKey().Address()).String(),
		}
	}
	return out
}

type councilKey struct {
	id  string
	key *sdked.PrivKey
	pub []byte
}

func publicSeats(seats []councilKey) []Seat {
	out := make([]Seat, len(seats))
	for i, seat := range seats {
		out[i] = Seat{
			SeatID:       seat.id,
			Address:      sdk.AccAddress(seat.key.PubKey().Address()).String(),
			PublicKeyHex: hex.EncodeToString(seat.pub),
		}
	}
	return out
}

func document(opts Options, nodes []prepared, accounts []accountKey, seats []councilKey) genesis.Document {
	validators := make([]genesis.Validator, len(nodes))
	for i, nodeHome := range nodes {
		validators[i] = genesis.Validator{
			NodeID:             nodeHome.name,
			Power:              opts.Power,
			ConsensusPubkeyHex: hex.EncodeToString(nodeHome.pub),
		}
	}
	rows := make([]genesis.BlockedDecision, len(genesis.DecisionIDs))
	for i, id := range genesis.DecisionIDs {
		rows[i] = genesis.BlockedDecision{DecisionID: id, SchemaVersion: 1}
	}
	submitters := make([]genesis.Submitter, 0, len(accounts)+len(seats))
	for _, account := range accounts {
		submitters = append(submitters, genesis.Submitter{
			AddressHex: hex.EncodeToString(account.key.PubKey().Address()),
			Role:       account.role,
		})
	}
	seatRows := make([]genesis.DevnetTestSeat, len(seats))
	for i, seat := range seats {
		submitters = append(submitters, genesis.Submitter{
			AddressHex: hex.EncodeToString(seat.key.PubKey().Address()),
			Role:       genesis.RoleBroadcaster,
		})
		seatRows[i] = genesis.DevnetTestSeat{SeatID: seat.id, PublicKeyHex: hex.EncodeToString(seat.pub)}
	}
	return genesis.Document{
		ChainID:                   opts.ChainID,
		GenesisUnixSeconds:        opts.GenesisTime.Unix(),
		ProtocolVersion:           genesis.ProtocolVersion,
		AllowDevnetTestParameters: true,
		Validators:                validators,
		Submitters:                submitters,
		DevnetTestSeats:           seatRows,
		BlockedDecisions:          rows,
		DevnetTestParameters: []genesis.DevnetTestParameter{
			{Label: genesis.DevnetTestLabel, DecisionID: "DEC-001", RoleName: "DEVNET_TEST_ONLY"},
			{Label: genesis.DevnetTestLabel, DecisionID: "DEC-002", RoleName: "DEVNET_TEST_ONLY"},
			// DEC-003 lets the council force-withdraw content on the devnet.
			{Label: genesis.DevnetTestLabel, DecisionID: "DEC-003", RoleName: "DEVNET_TEST_ONLY"},
			{Label: genesis.DevnetTestLabel, DecisionID: "DEC-005", NumericValue: 3600},
			// Four seats and three approvals is DEC-008. Delay 0 is the
			// devnet test parameter. Activation is still strictly later.
			{Label: genesis.DevnetTestLabel, DecisionID: "DEC-008", NumericValue: 3, DelaySeconds: 0},
		},
		SafetyCaps: genesis.DefaultSafetyCaps(),
	}
}

func cometValidators(nodes []prepared, power int64) []cmttypes.GenesisValidator {
	out := make([]cmttypes.GenesisValidator, len(nodes))
	for i, nodeHome := range nodes {
		pub := cmted25519.PubKey(nodeHome.pub)
		out[i] = cmttypes.GenesisValidator{
			Address: pub.Address(),
			PubKey:  pub,
			Power:   power,
			Name:    nodeHome.name,
		}
	}
	return out
}

func consensusParams() *cmttypes.ConsensusParams {
	params := cmttypes.DefaultConsensusParams()
	params.Block.MaxGas = int64(genesis.MaxGasPerTx)
	params.Version.App = genesis.ProtocolVersion
	return params
}

func writeKey(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(raw)+"\n"), 0o600)
}
