// Package node starts one svnd process from an initialized home directory.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	rpcclient "github.com/cometbft/cometbft/rpc/client/local"
	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/cometwrap"
)

// Listen is the public bind file written next to the CometBFT config.
type Listen struct {
	GRPC string `json:"grpc"`
	RPC  string `json:"rpc"`
	P2P  string `json:"p2p"`
}

// Start runs one validator until SIGINT or SIGTERM. It does not delete data.
func Start(home string) error {
	logger := log.NewLogger(os.Stdout).With("home", home)
	cfg, err := loadCometConfig(home)
	if err != nil {
		return err
	}
	chainID, err := readChainID(cfg.GenesisFile())
	if err != nil {
		return err
	}
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(home, "data"))
	if err != nil {
		return err
	}
	application, err := app.New(app.Config{
		Logger:  logger,
		DB:      db,
		ChainID: chainID,
	})
	if err != nil {
		_ = db.Close()
		return err
	}
	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.NodeKeyFile())
	if err != nil {
		_ = application.Close()
		return err
	}
	cmtApp := cometwrap.New(application)
	tmNode, err := node.NewNode(
		cfg,
		privval.LoadFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(cmtApp),
		node.DefaultGenesisDocProviderFunc(cfg),
		cmtcfg.DefaultDBProvider,
		node.DefaultMetricsProvider(cfg.Instrumentation),
		servercmtlog.CometLoggerWrapper{Logger: logger},
	)
	if err != nil {
		_ = application.Close()
		return err
	}
	if err := tmNode.Start(); err != nil {
		_ = application.Close()
		return err
	}
	clientCtx := client.Context{}.
		WithCodec(application.EncodingConfig().Codec).
		WithInterfaceRegistry(application.EncodingConfig().InterfaceRegistry).
		WithTxConfig(application.EncodingConfig().TxConfig).
		WithHomeDir(home).
		WithChainID(chainID).
		WithClient(rpcclient.New(tmNode))
	application.RegisterTxService(clientCtx)
	application.RegisterTendermintService(clientCtx)
	application.RegisterNodeService(clientCtx, application.ServerConfig())

	grpcServer := grpc.NewServer()
	application.RegisterGRPCServerWithSkipCheckHeader(grpcServer, true)
	var listener net.Listener
	if listen, err := readListen(home); err == nil && listen.GRPC != "" {
		listener, err = net.Listen("tcp", listen.GRPC)
		if err != nil {
			_ = tmNode.Stop()
			_ = application.Close()
			return err
		}
		go func() {
			if serveErr := grpcServer.Serve(listener); serveErr != nil {
				logger.Error("grpc stopped", "err", serveErr)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("stopping validator")
	grpcServer.GracefulStop()
	if listener != nil {
		_ = listener.Close()
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() {
		stopped <- tmNode.Stop()
	}()
	select {
	case err := <-stopped:
		_ = application.Close()
		return err
	case <-stopCtx.Done():
		_ = application.Close()
		return fmt.Errorf("validator stop timed out")
	}
}

// NodeFile is the bind and peer record start reads. config.toml is written
// from the same values for operators. Start does not reparse durations.
type NodeFile struct {
	Moniker          string `json:"moniker"`
	P2P              string `json:"p2p"`
	RPC              string `json:"rpc"`
	PersistentPeers  string `json:"persistent_peers"`
	AllowDuplicateIP bool   `json:"allow_duplicate_ip"`
	AddrBookStrict   bool   `json:"addr_book_strict"`
	Pex              bool   `json:"pex"`
}

func loadCometConfig(home string) (*cmtcfg.Config, error) {
	raw, err := os.ReadFile(filepath.Join(home, "config", "node.json"))
	if err != nil {
		return nil, err
	}
	var saved NodeFile
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot(home)
	cfg.Moniker = saved.Moniker
	cfg.P2P.ListenAddress = saved.P2P
	cfg.P2P.PersistentPeers = saved.PersistentPeers
	cfg.P2P.AllowDuplicateIP = saved.AllowDuplicateIP
	cfg.P2P.AddrBookStrict = saved.AddrBookStrict
	cfg.P2P.PexReactor = saved.Pex
	cfg.P2P.Seeds = ""
	cfg.RPC.ListenAddress = saved.RPC
	cfg.Instrumentation.Prometheus = false
	applyDevnetTimeouts(cfg)
	if err := cfg.ValidateBasic(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ApplyDevnetTimeouts sets the local devnet consensus timers.
// These are node timers, not application state and not a local clock read
// inside a transaction.
func ApplyDevnetTimeouts(cfg *cmtcfg.Config) {
	applyDevnetTimeouts(cfg)
}

func applyDevnetTimeouts(cfg *cmtcfg.Config) {
	cfg.Consensus.TimeoutPropose = 400 * time.Millisecond
	cfg.Consensus.TimeoutProposeDelta = 50 * time.Millisecond
	cfg.Consensus.TimeoutPrevote = 100 * time.Millisecond
	cfg.Consensus.TimeoutPrevoteDelta = 50 * time.Millisecond
	cfg.Consensus.TimeoutPrecommit = 100 * time.Millisecond
	cfg.Consensus.TimeoutPrecommitDelta = 50 * time.Millisecond
	cfg.Consensus.TimeoutCommit = 200 * time.Millisecond
	cfg.Consensus.SkipTimeoutCommit = true
	cfg.Consensus.CreateEmptyBlocks = true
	cfg.Consensus.CreateEmptyBlocksInterval = 0
}

func readChainID(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var header struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return "", err
	}
	if header.ChainID == "" {
		return "", fmt.Errorf("genesis %s has no chain id", path)
	}
	return header.ChainID, nil
}

func readListen(home string) (Listen, error) {
	raw, err := os.ReadFile(filepath.Join(home, "config", "svn-listen.json"))
	if err != nil {
		return Listen{}, err
	}
	var listen Listen
	if err := json.Unmarshal(raw, &listen); err != nil {
		return Listen{}, err
	}
	return listen, nil
}
