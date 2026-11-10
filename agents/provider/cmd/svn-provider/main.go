// Command svn-provider is the development storage-provider agent.
// It binds to loopback only. It does not use a production provider key.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/agent"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/cert"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/config"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/httpserver"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/keys"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
)

func main() {
	os.Exit(command(os.Args[1:]))
}

func command(args []string) int {
	if len(args) != 2 || (args[0] != "init" && args[0] != "run") {
		fmt.Fprintln(os.Stderr, "usage: svn-provider init|run config.json")
		return 2
	}
	cfg, err := config.Load(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	switch args[0] {
	case "init":
		err = initAgent(cfg)
	default:
		err = runAgent(cfg)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	return 0
}

func initAgent(cfg config.File) error {
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "keys"), 0o700); err != nil {
		return err
	}
	key, err := keys.Generate()
	if err != nil {
		return err
	}
	if err := keys.Write(store.KeyPath(cfg.DataDir, 1), key); err != nil {
		return err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "admin.token"), []byte(hex.EncodeToString(token[:])+"\n"), 0o600); err != nil {
		return err
	}
	if err := cert.Write(filepath.Join(cfg.DataDir, "tls.crt"), filepath.Join(cfg.DataDir, "tls.key")); err != nil {
		return err
	}
	pub := map[string]any{
		"provider_id": cfg.ProviderID, "key_id": cfg.KeyID,
		"public_key_hex": hex.EncodeToString(key.Public()),
		"address_hex":    hex.EncodeToString(key.Address()),
		"machine_id":     cfg.MachineID, "hosting_consent": true, "production_keys": false,
		"independent_operator": false,
	}
	body, err := json.MarshalIndent(pub, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg.DataDir, "enrollment.json"), append(body, '\n'), 0o644)
}

func runAgent(cfg config.File) error {
	a, err := agent.Open(cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !tcp.IP.IsLoopback() {
		return fmt.Errorf("listen is not loopback")
	}
	url := "https://" + tcp.AddrPort().String() + "/"
	a.SetURL(url)
	raw, err := json.Marshal(map[string]string{"url": url})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "listen.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() {
		errc <- (&httpserver.Server{Agent: a, Ln: ln}).Serve(
			filepath.Join(cfg.DataDir, "tls.crt"), filepath.Join(cfg.DataDir, "tls.key"))
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errc:
		return err
	case <-sig:
		return ln.Close()
	}
}
