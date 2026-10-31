package app

import (
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
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	sdkserverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/scaffold"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
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

// App is the Goal 05 chain. The six blueprint modules are registered.
// Provider, content, lease, evidence, and policy execution stay empty until
// Goals 06-10. The validator set is static.
type App struct {
	*baseapp.BaseApp

	encoding  Encoding
	mm        *module.Manager
	authority authority.Keeper
	config    sdkserverconfig.Config
}

// Config is the construction input. DB and ChainID are required.
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
	base := baseapp.NewBaseApp(
		Name,
		cfg.Logger,
		cfg.DB,
		enc.TxConfig.TxDecoder(),
		baseapp.SetChainID(cfg.ChainID),
		baseapp.EnableBlockGasMeter(),
		baseapp.SetMinGasPrices(""),
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
		runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		map[string][]string{},
		address.NewBech32Codec(chainaddress.Account),
		chainaddress.Account,
		authtypes.NewModuleAddress("svn").String(),
	)
	authModule := auth.NewAppModule(enc.Codec, accountKeeper, authsims.RandomGenesisAccounts)
	authorityKeeper := authority.NewKeeper(keys[authority.ModuleName])
	authorityModule := authority.NewAppModule(authorityKeeper, cfg.Order)

	svnModule := scaffold.New(ModuleSVN, keys[ModuleSVN], cfg.Order, checkHeader)
	policyModule := scaffold.New(ModulePolicy, keys[ModulePolicy], cfg.Order, checkPolicy)
	providersModule := scaffold.New(ModuleProviders, keys[ModuleProviders], cfg.Order, checkProviders)
	contentModule := scaffold.New(ModuleContent, keys[ModuleContent], cfg.Order, checkContent)
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
		authority: authorityKeeper,
		config:    *sdkserverconfig.DefaultConfig(),
	}
	app.config.MinGasPrices = ""
	app.config.API.Enable = false
	app.config.GRPC.Enable = true

	configurator := module.NewConfigurator(enc.Codec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	authModule.RegisterServices(configurator)
	registerApplicationMessages(app.MsgServiceRouter())

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
	}
	return app, nil
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
// Application-module REST routes are Goals 06-10.
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
	wrapped := server.NewCometABCIWrapper(app)
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

func checkProviders(raw json.RawMessage) error {
	var state providersv1.GenesisState
	if err := protojson.Unmarshal(raw, &state); err != nil {
		return err
	}
	if len(state.GetProviders()) != 0 {
		return fmt.Errorf("providers are not empty")
	}
	return nil
}

func checkContent(raw json.RawMessage) error {
	var state contentv1.GenesisState
	if err := protojson.Unmarshal(raw, &state); err != nil {
		return err
	}
	if len(state.GetContents()) != 0 || len(state.GetTombstones()) != 0 {
		return fmt.Errorf("content is not empty")
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
