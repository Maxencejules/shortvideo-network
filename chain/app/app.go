package app

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
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
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	"github.com/Maxencejules/shortvideo-network/chain/x/scaffold"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
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
// proof, and it never renews, extends, or revives a lease. Policy has no
// message service: a governed limit changes only through a council
// governed-limit payload.
type App struct {
	*baseapp.BaseApp

	encoding     Encoding
	mm           *module.Manager
	configurator module.Configurator
	keys         map[string]*storetypes.KVStoreKey
	authority    authority.Keeper
	config       sdkserverconfig.Config
}

// Config is the construction input. DB and ChainID are required.
// AppOptions are node options; New reads QueryGasLimitOption from them and
// nothing else.
type Config struct {
	Logger     log.Logger
	DB         dbm.DB
	ChainID    string
	Order      *authority.Order
	SkipLoad   bool
	AppOptions servertypes.AppOptions
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
	// parameters, governed limits, and the policy version from x/authority.
	// They hold the keeper copy taken before WithRegistry; that read surface
	// does not use the council hooks. x/authority then executes council
	// admit, disable, and force-withdraw payloads through the registries.
	// Business ids are one namespace across authority, providers, content,
	// leases, and evidence.
	readKeeper := authority.NewKeeper(keys[authority.ModuleName])
	peers := msgkit.NewPeers()
	providersKeeper := providers.NewKeeper(keys[ModuleProviders], providers.Deps{
		Roles:  readKeeper,
		Limits: readKeeper,
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
		Limits: readKeeper,
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
		Limits:    readKeeper,
		Params:    readKeeper,
		Policy:    readKeeper,
		Providers: providersKeeper,
		Content:   contentRead,
		Peers:     peers,
	})
	evidenceKeeper := evidence.NewKeeper(keys[ModuleEvidence], evidence.Deps{
		Roles:   readKeeper,
		Limits:  readKeeper,
		Leases:  leasesRead,
		Content: contentRead,
		Peers:   peers,
	})
	leasesKeeper := leases.NewKeeper(keys[ModuleLeases], leases.Deps{
		Roles:     readKeeper,
		Limits:    readKeeper,
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
		Limits: readKeeper,
		Peers:  peers,
		Hook:   leasesKeeper,
	})
	authorityKeeper := readKeeper.WithRegistry(providersKeeper, contentKeeper).WithPeers(peers)
	peers.Add(authority.ModuleName, authorityKeeper)
	peers.Add(content.ModuleName, contentKeeper)
	peers.Add(evidence.ModuleName, evidenceKeeper)
	peers.Add(leases.ModuleName, leasesKeeper)
	peers.Add(providers.ModuleName, providersKeeper)
	authorityModule := authority.NewAppModule(authorityKeeper, cfg.Order)

	svnModule := scaffold.New(ModuleSVN, keys[ModuleSVN], cfg.Order, checkHeader)
	policyModule := scaffold.New(ModulePolicy, keys[ModulePolicy], cfg.Order, checkPolicy)
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
	// BeginBlock runs in InitOrder: x/leases expiry (the only BeginBlock
	// work) runs after providers and content and before authority.
	// x/evidence has no BeginBlock and no EndBlock: evidence expiry is
	// computed at read time and nothing is swept.
	mm.SetOrderBeginBlockers(order...)
	mm.SetOrderEndBlockers(order...)
	mm.SetOrderPreBlockers(authtypes.ModuleName)

	app := &App{
		BaseApp:   base,
		encoding:  enc,
		mm:        mm,
		keys:      keys,
		authority: authorityKeeper,
		config:    *sdkserverconfig.DefaultConfig(),
	}
	app.config.MinGasPrices = ""
	app.config.API.Enable = false
	app.config.GRPC.Enable = true

	configurator := module.NewConfigurator(enc.Codec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	app.configurator = configurator
	authModule.RegisterServices(configurator)
	// Each registry, x/leases, and x/evidence registers svn.<module>.v1.Msg
	// on the message router, svn.<module>.v1.Query on the query router, and
	// its migrations.
	providersModule.RegisterServices(configurator)
	contentModule.RegisterServices(configurator)
	leasesModule.RegisterServices(configurator)
	evidenceModule.RegisterServices(configurator)
	registerAuthority(app.MsgServiceRouter(), authorityKeeper)

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

	if !cfg.SkipLoad {
		if err := app.LoadLatestVersion(); err != nil {
			return nil, err
		}
		if err := app.checkRegistryLayouts(); err != nil {
			return nil, err
		}
	}
	return app, nil
}

// checkRegistryLayouts refuses committed state whose providers, content,
// leases, or evidence store is not in this binary's layout. Every committed
// version of those stores holds msgkit.LayoutVersionKey, which InitGenesis
// writes. A store without it was written by the goal-05/06 scaffold slot
// (layout version 1: the module genesis JSON at key [0x01], inside the
// module's record prefix; for leases, the offer prefix), which this binary
// would misread; no migration from it exists, so the node does not start.
// It runs once at load, outside consensus, and only when a block has been
// committed.
func (app *App) checkRegistryLayouts() error {
	if app.LastBlockHeight() == 0 {
		return nil
	}
	for _, store := range []struct {
		name    string
		version uint64
	}{
		{ModuleProviders, providers.ConsensusVersion},
		{ModuleContent, content.ConsensusVersion},
		{ModuleLeases, leases.ConsensusVersion},
		{ModuleEvidence, evidence.ConsensusVersion},
	} {
		version, found, err := msgkit.ReadLayoutVersion(app.CommitMultiStore().GetKVStore(app.keys[store.name]))
		switch {
		case err != nil:
			return fmt.Errorf("%s store at height %d: %w", store.name, app.LastBlockHeight(), err)
		case !found:
			return fmt.Errorf("%s store at height %d has no layout version: it is the goal-05/06 scaffold layout (version %d), which this binary cannot read or migrate; initialize a new home",
				store.name, app.LastBlockHeight(), msgkit.ScaffoldLayoutVersion)
		case version != store.version:
			return fmt.Errorf("%s store at height %d has layout version %d, this binary reads version %d",
				store.name, app.LastBlockHeight(), version, store.version)
		}
	}
	return nil
}

func (app *App) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
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
	// The authority module's own InitGenesis cannot return an error and does
	// not see the start point, so its rules run here.
	if err := ValidateAuthorityGenesis(modules, req.InitialHeight, req.Time.Unix()); err != nil {
		return nil, err
	}
	return app.mm.InitGenesis(ctx, app.encoding.Codec, modules)
}

func (app *App) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	return app.mm.PreBlock(ctx)
}

func (app *App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.mm.BeginBlock(ctx)
}

func (app *App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.mm.EndBlock(ctx)
}

// Export returns the current committed application state as module genesis
// JSON. It refuses, with an error that starts "export refused:", in these
// cases (DR-06-01, DR-06-02, DR-07-02, DR-07-03, DR-08-06, DR-09-18):
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

func checkPolicy(raw json.RawMessage) error {
	var state policyv1.GenesisState
	if err := protojson.Unmarshal(raw, &state); err != nil {
		return err
	}
	caps := state.GetPolicy().GetSafetyCaps()
	if caps == nil {
		return fmt.Errorf("safety caps")
	}
	got := genesis.SafetyCaps{
		SafetyMaxPackageBytes:          caps.GetSafetyMaxPackageBytes(),
		SafetyMaxEvidenceBatch:         caps.GetSafetyMaxEvidenceBatch(),
		SafetyMaxExpiryStepsPerBlock:   caps.GetSafetyMaxExpiryStepsPerBlock(),
		SafetyMaxPageLimit:             caps.GetSafetyMaxPageLimit(),
		SafetyMaxTxBytes:               caps.GetSafetyMaxTxBytes(),
		SafetyMaxGasPerTx:              caps.GetSafetyMaxGasPerTx(),
		SafetyMaxOpenOffersPerProvider: caps.GetSafetyMaxOpenOffersPerProvider(),
	}
	if got != genesis.DefaultSafetyCaps() {
		return fmt.Errorf("safety caps")
	}
	return nil
}

var _ servertypes.Application = (*App)(nil)

// The app wiring satisfies the read surfaces each module declares.
var (
	_ leases.Evidence  = evidence.Keeper{}
	_ evidence.Leases  = leases.Keeper{}
	_ evidence.Content = content.Keeper{}
	_ evidence.Roles   = authority.Keeper{}
	_ evidence.Limits  = authority.Keeper{}
)
