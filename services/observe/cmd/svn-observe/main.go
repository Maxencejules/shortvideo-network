package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
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
	svc, err := obs.New(obs.Options{Token: os.Getenv("SVN_OBSERVE_TOKEN")})
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

func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
