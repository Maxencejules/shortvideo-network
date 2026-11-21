package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/observe/internal/obs"
)

func main() {
	os.Exit(run())
}

func run() int {
	listen := os.Getenv("SVN_OBSERVE_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:0"
	}
	if !loopback(listen) {
		fmt.Fprintln(os.Stderr, "listen address must be a loopback host")
		return 1
	}
	limits, queues, err := limitEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "observer limit is not a number")
		return 1
	}
	svc, err := obs.New(obs.Options{Token: os.Getenv("SVN_OBSERVE_TOKEN"), Limits: limits, Queues: queues})
	if err != nil {
		fmt.Fprintln(os.Stderr, "observer token is missing")
		return 1
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen failed")
		return 1
	}
	fmt.Printf("LISTEN %s\n", ln.Addr().String())
	server := &http.Server{
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "serve failed")
		return 1
	}
	return 0
}

func limitEnv() (map[string]int64, map[string]int64, error) {
	limits := map[string]int64{}
	queues := map[string]int64{}
	for _, kind := range []string{
		obs.KindUploads, obs.KindProcessing, obs.KindOutputBytes, obs.KindStorage,
		obs.KindAudit, obs.KindRetries, obs.KindLogin, obs.KindSearch, obs.KindReport, obs.KindDiscovery,
	} {
		if raw := os.Getenv("SVN_OBSERVE_LIMIT_" + kind); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, nil, err
			}
			limits[kind] = n
		}
		if raw := os.Getenv("SVN_OBSERVE_QUEUE_" + kind); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, nil, err
			}
			queues[kind] = n
		}
	}
	return limits, queues, nil
}

func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
