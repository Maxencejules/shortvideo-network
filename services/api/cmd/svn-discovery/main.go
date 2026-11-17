package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/social"
)

func main() {
	log.SetFlags(0)
	listen := "127.0.0.1:0"
	dev := false
	for _, arg := range os.Args[1:] {
		switch {
		case arg == "-dev":
			dev = true
		case len(arg) > 8 && arg[:8] == "-listen=":
			listen = arg[8:]
		default:
			log.Fatal("usage: svn-discovery [-dev] [-listen=127.0.0.1:0]")
		}
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		log.Fatal("listen must be loopback")
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("LISTEN %s\n", ln.Addr().String())
	_ = os.Stdout.Sync()
	server := &http.Server{
		Handler:           social.New(social.Options{Dev: dev}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
