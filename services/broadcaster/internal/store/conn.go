package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
)

// ConnConfig is everything needed to reach the database. It is turned into a
// pq.Config field by field and opened with pq.NewConnectorConfig, which reads
// no PG* environment variable and no ~/.pgpass (a non-empty Password stops
// lib/pq from looking for a password file).
//
// Only Unix-socket connections are supported here: the development cluster
// (internal/pgdev) listens on a socket only, and SSL does not apply to a
// socket.
type ConnConfig struct {
	// SocketDir is the absolute directory that holds .s.PGSQL.<Port>.
	SocketDir string
	// Port is the number in the socket file name.
	Port uint16
	// Database and User are PostgreSQL identifiers, ^[a-z_][a-z0-9_]{0,62}$.
	Database string
	User     string
	// Password is the role's password, at most 256 bytes, no NUL.
	Password string
	// ApplicationName shows in pg_stat_activity; printable ASCII, at most 63
	// bytes, may be empty.
	ApplicationName string
	// ConnectTimeout bounds each connection attempt, 1 s to 60 s.
	ConnectTimeout time.Duration
}

var appNameRE = regexp.MustCompile(`^[!-~]{0,63}$`)

// pqConfig validates c and builds the explicit pq.Config.
func (c ConnConfig) pqConfig() (pq.Config, error) {
	switch {
	case !filepath.IsAbs(c.SocketDir) || filepath.Clean(c.SocketDir) != c.SocketDir:
		return pq.Config{}, fmt.Errorf("%w: SocketDir must be a clean absolute path", ErrInvalid)
	case c.Port == 0:
		return pq.Config{}, fmt.Errorf("%w: Port", ErrInvalid)
	case !roleRE.MatchString(c.Database):
		return pq.Config{}, fmt.Errorf("%w: Database", ErrInvalid)
	case !roleRE.MatchString(c.User):
		return pq.Config{}, fmt.Errorf("%w: User", ErrInvalid)
	case c.Password == "" || len(c.Password) > 256 || strings.ContainsRune(c.Password, 0):
		return pq.Config{}, fmt.Errorf("%w: Password", ErrInvalid)
	case !appNameRE.MatchString(c.ApplicationName):
		return pq.Config{}, fmt.Errorf("%w: ApplicationName", ErrInvalid)
	case c.ConnectTimeout < time.Second || c.ConnectTimeout > 60*time.Second:
		return pq.Config{}, fmt.Errorf("%w: ConnectTimeout outside [1s, 60s]", ErrInvalid)
	}
	return pq.Config{
		Host:               c.SocketDir,
		Port:               c.Port,
		Database:           c.Database,
		User:               c.User,
		Password:           c.Password,
		ApplicationName:    c.ApplicationName,
		SSLMode:            pq.SSLModeDisable,
		ClientEncoding:     "UTF8",
		Datestyle:          "ISO, MDY",
		MinProtocolVersion: pq.ProtocolVersion30,
		MaxProtocolVersion: pq.ProtocolVersion30,
		ConnectTimeout:     c.ConnectTimeout,
	}, nil
}

// OpenDB returns a connection pool for c. It does not connect; the first
// query or Ping does.
func OpenDB(c ConnConfig) (*sql.DB, error) {
	cfg, err := c.pqConfig()
	if err != nil {
		return nil, err
	}
	connector, err := pq.NewConnectorConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("store: connector: %w", err)
	}
	return sql.OpenDB(connector), nil
}
