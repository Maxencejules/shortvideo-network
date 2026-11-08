package app

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/protobuf/encoding/protojson"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/server/api"
	sdkserverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/cometwrap"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	"github.com/Maxencejules/shortvideo-network/chain/x/scaffold"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// Name is the application name. It is not a token chain and it is not a release.
const Name = "svnd"

// InitOrder is the genesis initialization order. auth is first so accounts
// exist. authority is last and is the only module that returns validator
// updates.
func InitOrder() []string {
	return []string{
		authtypes.ModuleName,
		ModuleSVN,
		ModulePolicy,
		ModuleProviders,
		ModuleContent,
		ModuleLeases,
		ModuleEvidence,
		authority.ModuleName,
	}
}

// App is the beta chain. The six blueprint modules are registered.
// x/authority (Goal 06) owns the validator set and the council; it runs the
// council proposal flow and is the only module that returns validator
// updates. The set changes only when a council change is due in EndBlock.
// x/providers and x/content (Goal 07) are the provider and content
// registries. x/leases (Goal 08) holds lease offers, leases, and their
// capacity reservations. x/evidence (Goal 09) records signed audit
// attestations of held leases; a record is an attestation, not a storage
// proof, and it never renews, extends, or revives a lease. x/policy (Goal
// 10) holds the safety caps, the governed limits and their pending
// changes, the upgrade plan and the applied upgrades, the module version
// map, and the app version. Policy has no message service: a governed limit
// or an upgrade plan changes only through an executed council proposal.
// The upgrade runner (chain/upgrade) checks the binary against the
// committed state at load and in PreBlock of every block, and applies an
// upgrade at its plan height.
type App struct {
	*baseapp.BaseApp

	encoding     Encoding
	mm           *module.Manager
	configurator module.Configurator
	keys         map[string]*storetypes.KVStoreKey
	authority    authority.Keeper
	policy       policy.Keeper
	accounts     authkeeper.AccountKeeper
	upgradeList  []upgrade.Upgrade
	upgrades     *upgrade.Runner
	gate         upgrade.Decision
	config       sdkserverconfig.Config
}

// Config is the construction input. DB and ChainID are required.
// AppOptions are node options; New reads QueryGasLimitOption from them and
// nothing else. Upgrades are the upgrade handlers compiled into the binary
// (svnd compiles none). They also set the binary's app version P:
// AppVersion when the list is empty, otherwise the handlers' ToAppVersion;
// New refuses a list that upgrade.Binary.Validate refuses, so every handler
// goes from P-1 to P.
type Config struct {
	Logger     log.Logger
	DB         dbm.DB
	ChainID    string
	Order      *authority.Order
	SkipLoad   bool
	AppOptions servertypes.AppOptions
	Upgrades   []upgrade.Upgrade
}

const (
	// AppVersion is the app version P of a binary that compiles no upgrade
	// handler, such as svnd. It is stored in x/policy (0x04) and in
	// ConsensusParams.Version.App, and it is not genesis.ProtocolVersion
	// (the protocol schema line): an app version bump is not a protocol
	// schema change.
	AppVersion uint64 = 1

	// GenesisAppVersion is the app version every chain starts at. InitChain
	// refuses a binary whose P differs and genesis consensus params whose
	// Version.App differs, so an upgrade binary cannot initialize a chain:
	// a new node starts with the genesis binary and switches binaries at
	// the plan height.
	GenesisAppVersion uint64 = 1
)

// binaryAppVersion is the app version P of a binary with upgrades.
func binaryAppVersion(upgrades []upgrade.Upgrade) uint64 {
	if len(upgrades) == 0 {
		return AppVersion
	}
	return upgrades[0].ToAppVersion
}

// New constructs the application. Economic modules are not registered.
func New(cfg Config) (*App, error) {
	chainaddress.Configure()
	if cfg.Logger == nil {
		cfg.Logger = log.NewNopLogger()
	}
	if cfg.DB == nil {
		return nil, fmt.Errorf("application database is required")
	}
	if cfg.ChainID == "" {
		return nil, fmt.Errorf("chain id is required")
	}
	enc, err := MakeEncoding()
	if err != nil {
		return nil, err
	}
	queryGasLimit, err := QueryGasLimit(cfg.AppOptions)
	if err != nil {
		return nil, err
	}
	base := baseapp.NewBaseApp(
		Name,
		cfg.Logger,
		cfg.DB,
		enc.TxConfig.TxDecoder(),
		baseapp.SetChainID(cfg.ChainID),
		baseapp.EnableBlockGasMeter(),
		baseapp.SetMinGasPrices(""),
		// Every gRPC-routed query (ABCI query on a registered service path,
		// and the node gRPC server) runs on a gas meter limited to
		// queryGasLimit: DefaultQueryGasLimit unless node.json sets another
		// value. A node that sets 2^64-1 gets the SDK's unlimited meter.
		// Raw store queries (/store/<store>/key|subspace) bypass the query
		// router and are not metered.
		baseapp.SetQueryGasLimit(queryGasLimit),
	)
	base.SetInterfaceRegistry(enc.InterfaceRegistry)
	base.SetTxEncoder(enc.TxConfig.TxEncoder())

	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey,
		ModuleSVN,
		ModulePolicy,
		ModuleProviders,
		ModuleContent,
		ModuleLeases,
		ModuleEvidence,
		authority.ModuleName,
	)
	base.MountStores(
		keys[authtypes.StoreKey],
		keys[ModuleSVN],
		keys[ModulePolicy],
		keys[ModuleProviders],
		keys[ModuleContent],
		keys[ModuleLeases],
		keys[ModuleEvidence],
		keys[authority.ModuleName],
	)

	accountKeeper := authkeeper.NewAccountKeeper(
		enc.Codec,
		newKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		map[string][]string{},
		address.NewBech32Codec(chainaddress.Account),
		chainaddress.Account,
		authtypes.NewModuleAddress("svn").String(),
	)
	authModule := newAuthModule(accountKeeper)

	// The registries, x/leases, and x/evidence read roles, devnet
	// parameters, and the policy version from x/authority, and governed
	// limits from x/policy. They hold the authority keeper copy taken before
	// WithRegistry; that read surface does not use the council hooks.
	// x/policy reads the policy version and the blocked decision rows from
	// the same copy. x/authority then executes council admit, disable, and
	// force-withdraw payloads through the registries, governed limit and
	// upgrade payloads through x/policy, and creates the auth account of a
	// replaced council seat key through the account keeper. Business ids are
	// one namespace across authority, providers, content, leases, and
	// evidence.
	readKeeper := authority.NewKeeper(keys[authority.ModuleName])
	policyKeeper := policy.NewKeeper(keys[ModulePolicy], policy.Deps{
		Versions:  readKeeper,
		Decisions: readKeeper,
	})
	peers := msgkit.NewPeers()
	providersKeeper := providers.NewKeeper(keys[ModuleProviders], providers.Deps{
		Roles:  readKeeper,
		Limits: policyKeeper,
		Peers:  peers,
	})
	// x/leases reads content through a copy of the content keeper without
	// the withdrawal hook (CheckOfferable, CheckNotWithdrawn). Both copies
	// share the content store key. The copy that carries the hook is the
	// one the module, the message router, and x/authority use, so a
	// WithdrawContent message and a council force-withdraw both void the
	// content's open lease offers in the withdrawal transaction.
	contentRead := content.NewKeeper(keys[ModuleContent], content.Deps{
		Roles:  readKeeper,
		Params: readKeeper,
		Limits: policyKeeper,
		Peers:  peers,
	})
	// x/evidence reads leases through a copy of the leases keeper without
	// the evidence reader; it calls only Lease, one read of the leases
	// store. Both copies share the leases store key. The copy that carries
	// the evidence reader is the one the module, the message router, and
	// the content withdrawal hook use, so a renewal that names an
	// evidence_id reads the committed evidence record (and is always
	// rejected: FORBIDDEN, EXPIRED, or NOT_FOUND).
	leasesRead := leases.NewKeeper(keys[ModuleLeases], leases.Deps{
		Roles:     readKeeper,
		Limits:    policyKeeper,
		Params:    readKeeper,
		Policy:    readKeeper,
		Providers: providersKeeper,
		Content:   contentRead,
		Peers:     peers,
	})
	evidenceKeeper := evidence.NewKeeper(keys[ModuleEvidence], evidence.Deps{
		Roles:   readKeeper,
		Limits:  policyKeeper,
		Leases:  leasesRead,
		Content: contentRead,
		Peers:   peers,
	})
	leasesKeeper := leases.NewKeeper(keys[ModuleLeases], leases.Deps{
		Roles:     readKeeper,
		Limits:    policyKeeper,
		Params:    readKeeper,
		Policy:    readKeeper,
		Providers: providersKeeper,
		Content:   contentRead,
		Evidence:  evidenceKeeper,
		Peers:     peers,
	})
	contentKeeper := content.NewKeeper(keys[ModuleContent], content.Deps{
		Roles:  readKeeper,
		Params: readKeeper,
		Limits: policyKeeper,
		Peers:  peers,
		Hook:   leasesKeeper,
	})
	authorityKeeper := readKeeper.WithRegistry(providersKeeper, contentKeeper).WithPeers(peers).
		WithPolicy(policyKeeper).WithAccounts(seatAccounts{keeper: accountKeeper})
	peers.Add(authority.ModuleName, authorityKeeper)
	peers.Add(content.ModuleName, contentKeeper)
	peers.Add(evidence.ModuleName, evidenceKeeper)
	peers.Add(leases.ModuleName, leasesKeeper)
	peers.Add(providers.ModuleName, providersKeeper)
	authorityModule := authority.NewAppModule(authorityKeeper, cfg.Order)

	svnModule := scaffold.New(ModuleSVN, keys[ModuleSVN], cfg.Order, checkHeader)
	policyModule := policy.NewAppModule(policyKeeper, cfg.Order)
	providersModule := providers.NewAppModule(providersKeeper, cfg.Order)
	contentModule := content.NewAppModule(contentKeeper, cfg.Order)
	leasesModule := leases.NewAppModule(leasesKeeper, cfg.Order)
	evidenceModule := evidence.NewAppModule(evidenceKeeper, cfg.Order)

	mm := module.NewManager(
		authModule,
		svnModule,
		policyModule,
		providersModule,
		contentModule,
		leasesModule,
		evidenceModule,
		authorityModule,
	)
	order := InitOrder()
	mm.SetOrderInitGenesis(order...)
	mm.SetOrderExportGenesis(order...)
	// BeginBlock runs in InitOrder: x/policy applies the pending governed
	// changes due at this height (ApplyDue) before x/leases runs expiry, so
	// a value activated at height a governs that block's lease expiry and
	// every transaction of block a (x/policy deviation D-8). x/leases
	// expiry runs after providers and content and before authority.
	// x/evidence has no BeginBlock and no EndBlock: evidence expiry is
	// computed at read time and nothing is swept.
	mm.SetOrderBeginBlockers(order...)
	mm.SetOrderEndBlockers(order...)
	mm.SetOrderPreBlockers(authtypes.ModuleName)
	// The upgrade runner's migrations run the modules in InitOrder.
	mm.SetOrderMigrations(order...)

	app := &App{
		BaseApp:     base,
		encoding:    enc,
		mm:          mm,
		keys:        keys,
		authority:   authorityKeeper,
		policy:      policyKeeper,
		accounts:    accountKeeper,
		upgradeList: append([]upgrade.Upgrade(nil), cfg.Upgrades...),
		config:      *sdkserverconfig.DefaultConfig(),
	}
	app.config.MinGasPrices = ""
	app.config.API.Enable = false
	app.config.GRPC.Enable = true

	configurator := module.NewConfigurator(enc.Codec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	app.configurator = configurator
	authModule.RegisterServices(configurator)
	// Each registry, x/leases, and x/evidence registers svn.<module>.v1.Msg
	// on the message router, svn.<module>.v1.Query on the query router, and
	// its migrations. x/policy registers svn.policy.v1.Query (Query/Policy)
	// and its migrations; it has no message service. x/authority registers
	// svn.authority.v1.Query (Query/Governance) and its migrations here, and
	// svn.authority.v1.Msg through registerAuthority.
	policyModule.RegisterServices(configurator)
	providersModule.RegisterServices(configurator)
	contentModule.RegisterServices(configurator)
	leasesModule.RegisterServices(configurator)
	evidenceModule.RegisterServices(configurator)
	authorityModule.RegisterServices(configurator)
	registerAuthority(app.MsgServiceRouter(), authorityKeeper)
	if err := app.buildRunner(); err != nil {
		return nil, err
	}

	ante, err := NewAnteHandler(accountKeeper, enc.TxConfig.SignModeHandler(), func(ctx sdk.Context, addr []byte) authority.Admission {
		return authorityKeeper.Admission(ctx, addr)
	})
	if err != nil {
		return nil, err
	}
	app.SetAnteHandler(ante)
	app.SetParamStore(newConsensusParamsStore(keys[ModuleSVN]))
	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	// ABCI Info reports the app version of committed state only: New sets
	// it at load (checkLayoutsAndVersions), and every Commit sets it again
	// from the new committed state, never PreBlock. A committed app version
	// that does not decode panics here, as the x/policy keeper does
	// (integrity damage; PreBlock of that block has already failed on it).
	app.SetPrepareCheckStater(func(ctx sdk.Context) {
		app.SetProtocolVersion(app.policy.AppVersion(ctx))
	})
	app.SetVersion(fmt.Sprintf("%s app version %d", Name, binaryAppVersion(app.upgradeList)))

	if !cfg.SkipLoad {
		if err := app.load(); err != nil {
			return nil, err
		}
	}
	return app, nil
}

// load opens the latest committed version and runs the load gate on it.
func (app *App) load() error {
	if err := app.LoadLatestVersion(); err != nil {
		return err
	}
	return app.checkLayoutsAndVersions()
}

// layoutStore is a store that keeps a layout version key
// (msgkit.LayoutVersionKey) and the layout version its module's
// InitGenesis writes.
type layoutStore struct {
	name    string
	version uint64
}

// layoutStores are the stores with a layout version key. The svn header
// slot and the auth store keep none. InitChainer requires each store to
// hold its version after InitGenesis, equal to the binary's version map
// entry; the load gate and the upgrade runner read these keys
// (layoutReader) and compare them with the persisted version map. A store
// without the key was written by the goal-05/06 scaffold slot (or, for
// x/policy and x/authority, by a binary from before goal 10), which this
// binary cannot read or migrate.
func layoutStores() []layoutStore {
	return []layoutStore{
		{ModulePolicy, policy.ConsensusVersion},
		{ModuleProviders, providers.ConsensusVersion},
		{ModuleContent, content.ConsensusVersion},
		{ModuleLeases, leases.ConsensusVersion},
		{ModuleEvidence, evidence.ConsensusVersion},
		{authority.ModuleName, authority.ConsensusVersion},
	}
}

// checkLayoutsAndVersions is the load gate (chain/upgrade.Gate) on the
// committed state, outside consensus. With no committed block it sets the
// reported app version to GenesisAppVersion and returns: InitChain rules
// apply. Otherwise the gate reads the stored app version, the stored
// Version.App, the persisted module version map, every layout key of
// layoutStores, and the plan, and refuses this binary unless it is the
// binary for the next block (gate row 1), or the binary of the upgrade
// that applies in the next block (row 3, the upgrade window). A refusal
// (rows 2, 4, 5, 6) is an error, so svnd exits 1 before CometBFT starts. A
// committed row that does not decode, or a store read that fails, refuses
// with row 6. On a start, the reported app version is the stored one.
func (app *App) checkLayoutsAndVersions() error {
	height := app.LastBlockHeight()
	if height == 0 {
		app.SetProtocolVersion(GenesisAppVersion)
		return nil
	}
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID(), Height: height})
	decision, stored, err := app.gateOn(ctx, uint64(height))
	if err != nil {
		return err
	}
	app.gate = decision
	app.SetProtocolVersion(stored)
	if decision.Window {
		app.Logger().Info("starting in the upgrade window: the next block applies the upgrade",
			"upgrade", decision.Upgrade.Name, "height", height+1, "app_version", decision.Upgrade.ToAppVersion)
	}
	return nil
}

// gateOn runs the runner's gate on ctx at last committed height L and
// returns the decision and the stored app version. Every refusal, store
// read error, and keeper panic on a committed row that does not decode is
// returned as an error naming the gate row.
func (app *App) gateOn(ctx sdk.Context, height uint64) (decision upgrade.Decision, stored uint64, err error) {
	defer func() {
		if p := recover(); p != nil {
			decision, stored = upgrade.Decision{}, 0
			err = fmt.Errorf("load gate at height %d refused this binary (row 6): committed state does not decode: %v", height, p)
		}
	}()
	decision, err = app.upgrades.Gate(ctx, height)
	var refusal *upgrade.RefusalError
	switch {
	case errors.As(err, &refusal):
		return upgrade.Decision{}, 0, fmt.Errorf("load gate at height %d refused this binary (row %d): %s", height, refusal.Row, refusal.Msg)
	case err != nil:
		return upgrade.Decision{}, 0, fmt.Errorf("load gate at height %d refused this binary (row 6): %w", height, err)
	}
	return decision, app.policy.AppVersion(ctx), nil
}

// ValidateGenesisConsensusParams applies InitChain's genesis consensus
// params rule (validateGenesisConsensusParams) to params. `svnd genesis
// validate` passes the params CometBFT's handshake hands InitChain for the
// same genesis file (GenesisDoc.ConsensusParams.ToProto()), so a file the
// command accepts is not refused by this rule when a new node runs
// InitChain on it. A home that has started before runs the genesis
// document CometBFT stored in its state database, not the file.
func ValidateGenesisConsensusParams(params *cmtproto.ConsensusParams) error {
	return validateGenesisConsensusParams(&abci.RequestInitChain{ConsensusParams: params})
}

// validateGenesisConsensusParams is InitChain's rule on the genesis
// consensus params: Version.App must be GenesisAppVersion, and params or
// version params that are missing are refused. It reads only
// req.ConsensusParams.
func validateGenesisConsensusParams(req *abci.RequestInitChain) error {
	if req.ConsensusParams == nil || req.ConsensusParams.Version == nil || req.ConsensusParams.Version.App != GenesisAppVersion {
		return fmt.Errorf("genesis consensus params Version.App must be %d", GenesisAppVersion)
	}
	return nil
}

// InitChainer initializes a new chain. Before any module writes, it
// refuses a binary whose app version P is not GenesisAppVersion, genesis
// consensus params validateGenesisConsensusParams refuses, a document
// genesis.Validate refuses, and the module genesis ValidateAuthorityGenesis
// refuses (the authority rules, the policy genesis, and the blocked
// decision rows of both). After every module's InitGenesis it checks that
// each store of layoutStores holds its layout version, equal to the
// binary's version map entry, and that the policy version is 1 (authority
// genesis has no policy version key). Last it writes the binary's module
// version map and app version into x/policy (InitVersions).
func (app *App) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	binary := app.upgrades.Binary()
	if binary.AppVersion != GenesisAppVersion {
		return nil, fmt.Errorf("this binary runs app version %d, and a chain starts at app version %d: initialize a node with the genesis binary and switch binaries at the upgrade height",
			binary.AppVersion, GenesisAppVersion)
	}
	if err := validateGenesisConsensusParams(req); err != nil {
		return nil, err
	}
	var modules map[string]json.RawMessage
	if err := json.Unmarshal(req.AppStateBytes, &modules); err != nil {
		return nil, fmt.Errorf("malformed app state: %w", err)
	}
	doc, err := DocumentFromModules(modules)
	if err != nil {
		return nil, err
	}
	if err := genesis.Validate(doc, genesis.CometIdentity{
		ChainID:            req.ChainId,
		GenesisUnixSeconds: req.Time.Unix(),
	}); err != nil {
		return nil, err
	}
	// The authority and policy modules' own InitGenesis cannot return an
	// error, and authority's does not see the start point, so their rules
	// run here.
	if err := ValidateAuthorityGenesis(modules, req.InitialHeight, req.Time.Unix()); err != nil {
		return nil, err
	}
	res, err := app.mm.InitGenesis(ctx, app.encoding.Codec, modules)
	if err != nil {
		return nil, err
	}
	for _, store := range layoutStores() {
		version, found, err := msgkit.ReadLayoutVersion(ctx.KVStore(app.keys[store.name]))
		switch {
		case err != nil:
			return nil, fmt.Errorf("%s store after genesis: %w", store.name, err)
		case !found || version != store.version || binary.Modules[store.name] != version:
			return nil, fmt.Errorf("%s store after genesis has layout version %d (found %t); its module writes %d and this binary's version map says %d",
				store.name, version, found, store.version, binary.Modules[store.name])
		}
	}
	if v := app.authority.PolicyVersion(ctx); v != 1 {
		return nil, fmt.Errorf("policy version after genesis is %d, want 1", v)
	}
	if err := app.policy.InitVersions(ctx, binary.Modules, binary.AppVersion); err != nil {
		return nil, err
	}
	return res, nil
}

// PreBlocker runs the upgrade step first, before every module's PreBlock
// and before any transaction of the block: the per-block app-version rule
// and, at the plan height, the upgrade (chain/upgrade.Runner.PreBlock,
// deviation D-6). An error fails the block on this node before anything of
// it is written; a panic on a committed row that does not decode is the
// same error. An applied upgrade changed Version.App, so the response
// reports changed consensus params.
func (app *App) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	applied, err := app.upgradeStep(ctx)
	if err != nil {
		return nil, err
	}
	rsp, err := app.mm.PreBlock(ctx)
	if err != nil {
		return nil, err
	}
	rsp.ConsensusParamsChanged = rsp.ConsensusParamsChanged || applied
	return rsp, nil
}

// upgradeStep is the runner's PreBlock with a panic returned as an error.
func (app *App) upgradeStep(ctx sdk.Context) (applied bool, err error) {
	defer func() {
		if p := recover(); p != nil {
			applied, err = false, fmt.Errorf("upgrade step at height %d: committed state does not decode: %v", ctx.BlockHeight(), p)
		}
	}()
	return app.upgrades.PreBlock(ctx)
}

func (app *App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.mm.BeginBlock(ctx)
}

func (app *App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.mm.EndBlock(ctx)
}

// Export returns the current committed application state as module genesis
// JSON. It refuses, with an error that starts "export refused:", in these
// cases (DR-06-01, DR-06-02, DR-07-02, DR-07-03, DR-08-06, DR-09-18,
// DR-10-04):
//
//   - the evidence store holds any row besides its layout version (a record,
//     and with it a batch marker, a lease index row, an auditor window, and
//     a business id). The export context below has no block time, so the
//     evidence ExportGenesis cannot report a record's state and fails
//     (evidence.ErrNoBlockTime); the SDK module manager runs it in a
//     goroutine, where its panic would end the process instead of refusing.
//     This check runs first. A record exists only next to a held lease, so
//     the launch rule below would refuse such an export anyway.
//   - the authority store holds state the frozen genesis proto has no field
//     for (proposal records, business ids, the policy version, and the other
//     prefixes ExportState names). Dropping it would lose replay protection
//     and council state on import. The authority check runs before the module
//     manager export, because the module ExportGenesis can only panic.
//   - the policy store holds governance state the policy genesis proto has
//     no field for (a governed value, a pending change, a plan, an applied
//     upgrade, a plan name), or the app version or the policy version is
//     not 1 (policy.Keeper.ExportCheck). It runs before the module manager
//     export for the same reason. The version map is derived from the
//     binary and is not exported.
//   - the exported genesis does not validate, for example unequal validator
//     powers after a genesis pending power change applied. Import would
//     reject that file.
//   - the provider, content, lease offer, or lease catalogue is not empty:
//     DocumentFromModules applies the goal 05 launch rule (an empty
//     catalogue at genesis). Provider and content business ids exist only
//     next to a record, and a lease business id or used-offer marker only on
//     a chain that has a provider record (no provider record is ever
//     deleted), so App.Export can never drop them silently.
//
// A fresh chain with no such state round-trips byte for byte.
func (app *App) Export() (map[string]json.RawMessage, error) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	if err := app.evidenceStoreEmpty(ctx); err != nil {
		return nil, fmt.Errorf("export refused: %w", err)
	}
	if _, err := app.authority.Export(ctx); err != nil {
		return nil, fmt.Errorf("export refused: %w", err)
	}
	if err := app.policy.ExportCheck(ctx); err != nil {
		return nil, fmt.Errorf("export refused: %w", err)
	}
	out, err := app.mm.ExportGenesis(ctx, app.encoding.Codec)
	if err != nil {
		return nil, fmt.Errorf("export refused: %w", err)
	}
	doc, err := DocumentFromModules(out)
	if err != nil {
		return nil, fmt.Errorf("export refused: exported genesis does not validate: %w", err)
	}
	if err := genesis.Validate(doc, genesis.CometIdentity{ChainID: app.ChainID()}); err != nil {
		return nil, fmt.Errorf("export refused: exported genesis does not validate: %w", err)
	}
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(out[authority.ModuleName], &state); err != nil {
		return nil, fmt.Errorf("export refused: exported genesis does not validate: %w", err)
	}
	if err := authority.ValidateGenesisState(&state, nil); err != nil {
		return nil, fmt.Errorf("export refused: exported genesis does not validate: %w", err)
	}
	return out, nil
}

// evidenceStoreEmpty fails when the evidence store holds a key other than
// msgkit.LayoutVersionKey. It reads at most two keys.
func (app *App) evidenceStoreEmpty(ctx sdk.Context) error {
	it := ctx.KVStore(app.keys[ModuleEvidence]).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		if !bytes.Equal(it.Key(), msgkit.LayoutVersionKey) {
			return fmt.Errorf("the evidence store holds evidence state (key %x): records are exported only with their reported state, which needs a committed block time, and a launch genesis carries no evidence", it.Key())
		}
	}
	return nil
}

// RegisterAPIRoutes registers the tx, CometBFT, and node gateway routes.
// The application modules have no REST gateway routes: the REST API is
// disabled, and registry queries are served by ABCI query and gRPC.
func (app *App) RegisterAPIRoutes(apiSvr *api.Server, _ sdkserverconfig.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
}

// RegisterTxService registers the transaction query service.
func (app *App) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.encoding.InterfaceRegistry)
}

// RegisterTendermintService registers CometBFT queries.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	wrapped := cometwrap.New(app)
	cmtservice.RegisterTendermintService(clientCtx, app.GRPCQueryRouter(), app.encoding.InterfaceRegistry, wrapped.Query)
}

// RegisterNodeService registers the node query service.
func (app *App) RegisterNodeService(clientCtx client.Context, cfg sdkserverconfig.Config) {
	nodeservice.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg, func() int64 {
		return app.CommitMultiStore().EarliestVersion()
	})
}

// ModuleNames returns the registered module names in initialization order.
func (app *App) ModuleNames() []string {
	return append([]string(nil), app.mm.OrderInitGenesis...)
}

// EncodingConfig returns the codecs. Tests use it to build transactions.
func (app *App) EncodingConfig() Encoding {
	return app.encoding
}

// ServerConfig returns the service configuration. Gas has no coin price.
func (app *App) ServerConfig() sdkserverconfig.Config {
	return app.config
}

// AuthorityConsensusBytes is the authority module's committed state key.
// The bytes are proto.MarshalOptions{Deterministic:true}, not protojson.
func (app *App) AuthorityConsensusBytes() ([]byte, error) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	return app.authority.ConsensusBytes(ctx)
}

// SnapshotStores is the order of the stores FullSnapshot concatenates:
// every mounted store except the auth account store, in genesis order.
// Account sequences change on every included transaction, rejected or not.
func SnapshotStores() []string {
	return []string{
		ModuleSVN,
		ModulePolicy,
		ModuleProviders,
		ModuleContent,
		ModuleLeases,
		ModuleEvidence,
		authority.ModuleName,
	}
}

// ModuleSnapshot is every committed key and value of one mounted store, as
// msgkit.StoreSnapshot encodes them. Reject tests compare it with
// bytes.Equal. An unknown store name returns nil.
func (app *App) ModuleSnapshot(name string) []byte {
	key, ok := app.keys[name]
	if !ok {
		return nil
	}
	return msgkit.StoreSnapshot(app.CommitMultiStore().GetKVStore(key))
}

// FullSnapshot concatenates ModuleSnapshot for every store in
// SnapshotStores. Each part is the store name and the snapshot, both
// length-prefixed (8-byte big endian), so two different states never share
// one encoding.
func (app *App) FullSnapshot() []byte {
	var out []byte
	for _, name := range SnapshotStores() {
		part := app.ModuleSnapshot(name)
		out = binary.BigEndian.AppendUint64(out, uint64(len(name)))
		out = append(out, name...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(part)))
		out = append(out, part...)
	}
	return out
}

func checkHeader(raw json.RawMessage) error {
	var header Header
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.ProtocolVersion != genesis.ProtocolVersion {
		return fmt.Errorf("protocol_version")
	}
	if header.MintModuleEnabled || header.DistributionModuleEnabled || header.SlashingModuleEnabled || header.StakingModuleEnabled || header.PermissionlessValidatorCreation {
		return fmt.Errorf("disabled module flag is set")
	}
	if header.ChainID == "" {
		return fmt.Errorf("chain_id")
	}
	return nil
}

var _ servertypes.Application = (*App)(nil)

// The app wiring satisfies the read surfaces each module declares.
var (
	_ leases.Evidence         = evidence.Keeper{}
	_ evidence.Leases         = leases.Keeper{}
	_ evidence.Content        = content.Keeper{}
	_ evidence.Roles          = authority.Keeper{}
	_ evidence.Limits         = policy.Keeper{}
	_ authority.PolicyKeeper  = policy.Keeper{}
	_ authority.AccountKeeper = seatAccounts{}
	_ policy.VersionSource    = authority.Keeper{}
	_ policy.DecisionSource   = authority.Keeper{}
)
