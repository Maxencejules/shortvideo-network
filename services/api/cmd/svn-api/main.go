package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/service"
	"github.com/lib/pq"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Print(err.Error())
		os.Exit(1)
	}
}

func run() error {
	path := os.Getenv("SVN_API_CONFIG")
	if path == "" {
		return errors.New("SVN_API_CONFIG is required")
	}
	file, err := service.LoadFile(path)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(getenv("SVN_API_PGPORT", "5432"))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("SVN_API_PGPORT is not a port")
	}
	delay, err := probeDelay()
	if err != nil {
		return err
	}
	quarantine := os.Getenv("SVN_API_QUARANTINE_DIR")
	if quarantine == "" {
		quarantine = file.QuarantineDir
	}
	listen := os.Getenv("SVN_API_LISTEN")
	if listen == "" {
		listen = file.Listen
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	svc, err := service.New(ctx, service.Options{
		Environment:       file.Environment,
		EnableDevFixtures: file.EnableDevFixtures,
		PublicLaunch:      file.PublicLaunch,
		Fixtures:          file.Fixtures,
		Limits:            file.Limits,
		QuarantineDir:     quarantine,
		Database: pq.Config{
			Host:     os.Getenv("SVN_API_PGHOST"),
			Port:     uint16(port),
			Database: os.Getenv("SVN_API_PGDATABASE"),
			User:     os.Getenv("SVN_API_PGUSER"),
			Password: os.Getenv("SVN_API_PGPASSWORD"),
		},
		ProbeDelay: delay,
	})
	if err != nil {
		return err
	}
	defer svc.Close()
	if err := svc.Resume(ctx); err != nil {
		return err
	}
	if !loopback(listen) {
		return errors.New("listen address must be a loopback host")
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	if addrFile := os.Getenv("SVN_API_ADDR_FILE"); addrFile != "" {
		if err := os.WriteFile(addrFile, []byte(ln.Addr().String()), 0o600); err != nil {
			_ = ln.Close()
			return err
		}
	}
	server := &http.Server{
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       time.Minute,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ln) }()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shut)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func probeDelay() (time.Duration, error) {
	value := os.Getenv("SVN_API_PROBE_DELAY")
	if value == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
