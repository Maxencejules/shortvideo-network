package app

import (
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
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	"github.com/Maxencejules/shortvideo-network/chain/x/scaffold"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
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
// x/authority (Goal 06) owns the validator set and the council. x/providers
// and x/content (Goal 07) are the provider and content registries. Lease,
// evidence, and policy execution stay closed until Goals 08-10.
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
		// Every query runs on a finite gas meter (DefaultQueryGasLimit
		// unless the node sets another value).
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

	// The registries read roles, devnet parameters, and governed limits from
	// x/authority. They hold the keeper copy taken before WithRegistry; that
	// read surface does not use the council hooks. x/authority then executes
	// council admit, disable, and force-withdraw payloads through the
	// registries. Business ids are one namespace across the three modules.
	readKeeper := authority.NewKeeper(keys[authority.ModuleName])
	peers := msgkit.NewPeers()
	providersKeeper := providers.NewKeeper(keys[ModuleProviders], providers.Deps{
		Roles:  readKeeper,
		Limits: readKeeper,
		Peers:  peers,
	})
	contentKeeper := content.NewKeeper(keys[ModuleContent], content.Deps{
		Roles:  readKeeper,
		Params: readKeeper,
		Limits: readKeeper,
		Peers:  peers,
		// Hook is nil: no lease offer store exists before Goal 08.
	})
	authorityKeeper := readKeeper.WithRegistry(providersKeeper, contentKeeper).WithPeers(peers)
	peers.Add(authority.ModuleName, authorityKeeper)
	peers.Add(content.ModuleName, contentKeeper)
	peers.Add(providers.ModuleName, providersKeeper)
	authorityModule := authority.NewAppModule(authorityKeeper, cfg.Order)

	svnModule := scaffold.New(ModuleSVN, keys[ModuleSVN], cfg.Order, checkHeader)
	policyModule := scaffold.New(ModulePolicy, keys[ModulePolicy], cfg.Order, checkPolicy)
	providersModule := providers.NewAppModule(providersKeeper, cfg.Order)
	contentModule := content.NewAppModule(contentKeeper, cfg.Order)
	leasesModule := scaffold.New(ModuleLeases, keys[ModuleLeases], cfg.Order, checkLeases)
	evidenceModule := scaffold.New(ModuleEvidence, keys[ModuleEvidence], cfg.Order, checkEvidence)

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
	// Each registry registers svn.<module>.v1.Msg on the message router,
	// svn.<module>.v1.Query on the query router, and its migrations.
	providersModule.RegisterServices(configurator)
	contentModule.RegisterServices(configurator)
	registerApplicationMessages(app.MsgServiceRouter(), authorityKeeper)

	ante, err := NewAnteHandler(accountKeeper, enc.TxConfig.SignModeHandler(), func(ctx sdk.Context, addr []byte) bool {
		return authorityKeeper.Admitted(ctx, addr)
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

// checkRegistryLayouts refuses committed state whose registry stores are
// not in this binary's layout. Every committed version of a registry store
// holds msgkit.LayoutVersionKey, which InitGenesis writes. A store without
// it was written by the goal-05/06 scaffold slot (layout version 1: the
// module genesis JSON at key [0x01], inside the registry's record prefix),
// which this binary would misread; no migration from it exists, so the
// node does not start. It runs once at load, outside consensus, and only
// when a block has been committed.
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

// Export returns the current committed application state.
func (app *App) Export() (map[string]json.RawMessage, error) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	return app.mm.ExportGenesis(ctx, app.encoding.Codec)
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

// AuthoritySnapshot is the state key and the side document. Reject tests
// compare it with bytes.Equal.
func (app *App) AuthoritySnapshot() []byte {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	return app.authority.ConsensusSnapshot(ctx)
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

// InstallMismatchedRemovalForTest writes a due removal with the wrong key.
// It is not a consensus message. The next EndBlock must halt.
func (app *App) InstallMismatchedRemovalForTest() error {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	return app.authority.InstallMismatchedRemoval(ctx)
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

func checkLeases(raw json.RawMessage) error {
	var state leasesv1.GenesisState
	if err := protojson.Unmarshal(raw, &state); err != nil {
		return err
	}
	if len(state.GetOffers()) != 0 || len(state.GetLeases()) != 0 {
		return fmt.Errorf("leases are not empty")
	}
	return nil
}

func checkEvidence(raw json.RawMessage) error {
	var state evidencev1.GenesisState
	if err := protojson.Unmarshal(raw, &state); err != nil {
		return err
	}
	if len(state.GetRecords()) != 0 || state.GetCurrentEpoch() != 0 {
		return fmt.Errorf("evidence is not empty")
	}
	return nil
}

var _ servertypes.Application = (*App)(nil)
