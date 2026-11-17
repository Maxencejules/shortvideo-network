// Command svn-operator is the loopback operator dashboard.
// It does not load a consensus private key and it does not submit governance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/operator/dash"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: svn-operator config.json")
		return 2
	}
	cfg, err := load(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	svc, err := dash.New(cfg.service)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	publicLn, err := listenLoopback(cfg.file.PublicListen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	adminLn, err := listenLoopback(cfg.file.AdminListen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	pub := "http://" + publicLn.Addr().String()
	adm := "http://" + adminLn.Addr().String()
	svc.SetOrigins(pub, adm)
	fmt.Printf("PUBLIC %s\n", publicLn.Addr().String())
	fmt.Printf("ADMIN %s\n", adminLn.Addr().String())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 2)
	go func() { errc <- serve(ctx, publicLn, svc.PublicHandler()) }()
	go func() { errc <- serve(ctx, adminLn, svc.AdminHandler()) }()
	select {
	case <-ctx.Done():
		return 0
	case err := <-errc:
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		return 0
	}
}

type fileConfig struct {
	PublicListen       string `json:"public_listen"`
	AdminListen        string `json:"admin_listen"`
	DevSessions        bool   `json:"dev_sessions"`
	WebDir             string `json:"web_dir"`
	ChainID            string `json:"chain_id"`
	Provider           *peer  `json:"provider"`
	Successor          *peer  `json:"successor"`
	IndexerURL         string `json:"indexer_url"`
	DiscoveryURL       string `json:"discovery_url"`
	DiscoveryTokenFile string `json:"discovery_token_file"`
	MediaQueue         string `json:"media_queue"`
	GovernanceURL      string `json:"governance_url"`
	SchedulerURL       string `json:"scheduler_url"`
}

type peer struct {
	ID             string `json:"id"`
	BaseURL        string `json:"base_url"`
	CAFile         string `json:"ca_file"`
	AdminTokenFile string `json:"admin_token_file"`
}

type loaded struct {
	file    fileConfig
	service dash.Config
}

func load(path string) (loaded, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return loaded{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var file fileConfig
	if err := dec.Decode(&file); err != nil {
		return loaded{}, err
	}
	token := ""
	if file.DiscoveryTokenFile != "" {
		buf, err := os.ReadFile(file.DiscoveryTokenFile)
		if err != nil {
			return loaded{}, err
		}
		token = strings.TrimSpace(string(buf))
	}
	cfg := dash.Config{
		DevSessions: file.DevSessions, WebDir: file.WebDir, ChainID: file.ChainID,
		IndexerURL: file.IndexerURL, DiscoveryURL: file.DiscoveryURL, DiscoveryToken: token,
		MediaQueue: file.MediaQueue, GovernanceURL: file.GovernanceURL, SchedulerURL: file.SchedulerURL,
	}
	if file.Provider != nil {
		cfg.Provider = &dash.Peer{ID: file.Provider.ID, BaseURL: file.Provider.BaseURL, CAFile: file.Provider.CAFile, TokenFile: file.Provider.AdminTokenFile}
	}
	if file.Successor != nil {
		cfg.Successor = &dash.Peer{ID: file.Successor.ID, BaseURL: file.Successor.BaseURL, CAFile: file.Successor.CAFile, TokenFile: file.Successor.AdminTokenFile}
	}
	return loaded{file: file, service: cfg}, nil
}

func listenLoopback(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !tcp.IP.IsLoopback() {
		ln.Close()
		return nil, errors.New("listen is not loopback")
	}
	return ln, nil
}

func serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
