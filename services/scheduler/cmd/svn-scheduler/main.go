// Command svn-scheduler serves the off-chain availability and distribution API.
// It does not sign for providers. Public launch is disabled.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/sched"
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
	coordinator := sched.New(sched.Config{
		PolicyID: file.PolicyID, PhysicalDomain: file.PhysicalDomain,
		IndexerURL: file.IndexerURL, Hosts: file.Hosts,
	})
	server := &http.Server{Addr: file.Listen, Handler: coordinator.Handler(), ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	return 0
}
