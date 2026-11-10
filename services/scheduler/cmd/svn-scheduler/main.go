// Command svn-scheduler serves the off-chain availability and distribution API.
// It does not sign for providers. Public launch is disabled.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/sched"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/stage"
)

func main() {
	os.Exit(command(os.Args[1:]))
}

func command(args []string) int {
	if len(args) != 2 || args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: svn-scheduler serve config.json")
		return 2
	}
	raw, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	var file struct {
		Listen         string       `json:"listen"`
		IndexerURL     string       `json:"indexer_url"`
		PolicyID       string       `json:"policy_id"`
		PhysicalDomain string       `json:"physical_failure_domain"`
		OfferCommitURL string       `json:"offer_commit_url"`
		StageToken     string       `json:"stage_token"`
		PackageRoot    string       `json:"package_root"`
		ConfirmSeconds int          `json:"confirm_seconds"`
		Hosts          []sched.Host `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	if file.Listen == "" || file.IndexerURL == "" || file.PhysicalDomain == "" {
		fmt.Fprintln(os.Stderr, "listen, indexer_url, and physical_failure_domain are required")
		return 1
	}
	host, _, err := net.SplitHostPort(file.Listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		fmt.Fprintln(os.Stderr, "listen must be a loopback address")
		return 1
	}
	for i := range file.Hosts {
		path := file.Hosts[i].AdminTokenFile
		if path == "" {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "admin token file")
			return 1
		}
		file.Hosts[i].AdminToken = strings.TrimSpace(string(body))
		file.Hosts[i].AdminTokenFile = ""
	}
	cfg := sched.Config{
		PolicyID: file.PolicyID, PhysicalDomain: file.PhysicalDomain,
		IndexerURL: file.IndexerURL, Hosts: file.Hosts,
		OfferCommitURL: file.OfferCommitURL, PackageRoot: file.PackageRoot,
	}
	if file.ConfirmSeconds > 0 {
		cfg.ConfirmFor = time.Duration(file.ConfirmSeconds) * time.Second
	}
	if file.StageToken != "" {
		src, err := stage.Listen(file.StageToken)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		defer src.Close()
		cfg.Stage = src
		caFile, err := os.CreateTemp("", "svn-stage-*.crt")
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if _, err := caFile.Write(src.CAPEM); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if err := caFile.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Printf("stage_url=%s\nstage_ca=%s\n", src.URL, filepath.Clean(caFile.Name()))
	}
	coordinator := sched.New(cfg)
	server := &http.Server{Addr: file.Listen, Handler: coordinator.Handler(), ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	return 0
}
