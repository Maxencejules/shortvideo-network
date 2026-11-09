package config

import (
	"errors"
	"sort"
	"strings"
)

// ErrEnvironment is a process environment that could change or reveal the
// database connection: a PG* variable or PQGO_DEBUG.
var ErrEnvironment = errors.New("config: the environment sets a variable that lib/pq reads")

// CheckEnvironment refuses an environment (os.Environ() form, NAME=value)
// that sets any variable whose name starts with PG, or PQGO_DEBUG (plan
// 7.7 and section 11). lib/pq's NewConfig and NewConnector merge PGHOST,
// PGPASSWORD, PGPASSFILE, PGOPTIONS, PGSSLMODE, PGSERVICE and the other PG*
// variables into a connection, and PQGO_DEBUG=1, read when the package is
// initialised, prints the connection settings and protocol traffic to
// stderr outside any logger. The broadcaster builds its pq.Config field by
// field and opens it with pq.NewConnectorConfig, which reads none of them;
// this refusal is the second guard, run by every subcommand before any
// connection. The error names the variables, never their values.
func CheckEnvironment(environ []string) error {
	var names []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "PG") || name == "PQGO_DEBUG" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return errors.Join(ErrEnvironment, errors.New("config: unset "+strings.Join(names, ", ")))
}
