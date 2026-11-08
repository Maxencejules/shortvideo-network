package store

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestLimitsValidate: every limit is checked against its section 11 hard
// bounds, inclusive, and MinLifetime may not exceed MaxLifetime.
func TestLimitsValidate(t *testing.T) {
	if err := testLimits().Validate(); err != nil {
		t.Fatalf("PROPOSED values refused: %v", err)
	}
	type edge struct {
		name   string
		set    func(*Limits, int64)
		lo, hi int64
	}
	d := func(f func(*Limits, time.Duration)) func(*Limits, int64) {
		return func(l *Limits, v int64) { f(l, time.Duration(v)) }
	}
	edges := []edge{
		{"TransitionsPerIntent", func(l *Limits, v int64) { l.TransitionsPerIntent = int(v) }, 16, 4096},
		{"ObservationBytes", func(l *Limits, v int64) { l.ObservationBytes = int(v) }, 256, 8192},
		{"AckLogBytes", func(l *Limits, v int64) { l.AckLogBytes = int(v) }, 0, 4096},
		{"AttemptsPerIntent", func(l *Limits, v int64) { l.AttemptsPerIntent = int(v) }, 1, 32},
		{"AttemptsPerBusinessID", func(l *Limits, v int64) { l.AttemptsPerBusinessID = int(v) }, 1, 16},
		{"BusinessAttempts", func(l *Limits, v int64) { l.BusinessAttempts = int(v) }, 1, 8},
		{"AuthContextBytes", func(l *Limits, v int64) { l.AuthContextBytes = int(v) }, 256, 8192},
		{"EnqueueLockTimeout", d(func(l *Limits, v time.Duration) { l.EnqueueLockTimeout = v }),
			int64(100 * time.Millisecond), int64(30 * time.Second)},
		{"EnqueueStatementTimeout", d(func(l *Limits, v time.Duration) { l.EnqueueStatementTimeout = v }),
			int64(time.Second), int64(60 * time.Second)},
	}
	for _, e := range edges {
		for _, c := range []struct {
			v  int64
			ok bool
		}{{e.lo, true}, {e.hi, true}, {e.lo - 1, false}, {e.hi + 1, false}} {
			l := testLimits()
			e.set(&l, c.v)
			if err := l.Validate(); (err == nil) != c.ok {
				t.Errorf("%s=%d: ok=%v, got %v", e.name, c.v, c.ok, err)
			}
		}
	}
	l := testLimits()
	l.MinLifetime, l.MaxLifetime = 7*24*time.Hour, 7*24*time.Hour
	if err := l.Validate(); err != nil {
		t.Errorf("lifetimes at the upper bound: %v", err)
	}
	for name, f := range map[string]func(*Limits){
		"min below 5s":    func(l *Limits) { l.MinLifetime = 5*time.Second - 1 },
		"max above 7d":    func(l *Limits) { l.MaxLifetime = 7*24*time.Hour + 1 },
		"min above max":   func(l *Limits) { l.MinLifetime, l.MaxLifetime = time.Hour, time.Minute },
		"zero everything": func(l *Limits) { *l = Limits{} },
	} {
		l := testLimits()
		f(&l)
		if err := l.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}

// TestNewRefuses: a nil pool, invalid limits and an instance outside
// printable ASCII of 1 to 64 bytes are refused.
func TestNewRefuses(t *testing.T) {
	if _, err := New(nil, testLimits(), "i"); !errors.Is(err, ErrInvalid) {
		t.Errorf("nil db: %v", err)
	}
	e := newRawEnv(t)
	if _, err := New(e.runtime, Limits{}, "i"); !errors.Is(err, ErrInvalid) {
		t.Errorf("zero limits: %v", err)
	}
	for _, inst := range []string{"", "has space", strings.Repeat("i", 65), "é"} {
		if _, err := New(e.runtime, testLimits(), inst); !errors.Is(err, ErrInvalid) {
			t.Errorf("instance %q: %v", inst, err)
		}
	}
}

// TestBusinessIDFormat (DR-11-07): drawn business ids are "bcast" plus 26
// lowercase base32 characters (128 random bits), match the chain id pattern,
// and do not repeat.
func TestBusinessIDFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 2000 {
		id, err := newBusinessID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 31 || !strings.HasPrefix(id, businessIDPrefix) || !idRE.MatchString(id) {
			t.Fatalf("business id %q", id)
		}
		if seen[id] {
			t.Fatalf("repeated business id %q", id)
		}
		seen[id] = true
	}
}

// TestConnConfigRefuses: every ConnConfig field is checked before a
// connector is built.
func TestConnConfigRefuses(t *testing.T) {
	good := ConnConfig{SocketDir: "/tmp/svn-pg-x/run", Port: 5432, Database: "db", User: "u", Password: "p",
		ApplicationName: "app", ConnectTimeout: 10 * time.Second}
	if _, err := OpenDB(good); err != nil {
		t.Fatalf("good config: %v", err)
	}
	for name, f := range map[string]func(*ConnConfig){
		"relative socket": func(c *ConnConfig) { c.SocketDir = "tmp/run" },
		"unclean socket":  func(c *ConnConfig) { c.SocketDir = "/tmp/run/" },
		"zero port":       func(c *ConnConfig) { c.Port = 0 },
		"bad database":    func(c *ConnConfig) { c.Database = "Db" },
		"bad user":        func(c *ConnConfig) { c.User = "u;x" },
		"empty password":  func(c *ConnConfig) { c.Password = "" },
		"long password":   func(c *ConnConfig) { c.Password = strings.Repeat("p", 257) },
		"nul password":    func(c *ConnConfig) { c.Password = "p\x00" },
		"bad app name":    func(c *ConnConfig) { c.ApplicationName = "a b" },
		"short timeout":   func(c *ConnConfig) { c.ConnectTimeout = time.Second - 1 },
		"long timeout":    func(c *ConnConfig) { c.ConnectTimeout = 61 * time.Second },
	} {
		c := good
		f(&c)
		if _, err := OpenDB(c); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}

// TestOpenIgnoresPGEnvironment (7.7): OpenDB builds an explicit pq.Config,
// so PG* variables pointing elsewhere change neither the server, the role,
// the database nor the session settings.
func TestOpenIgnoresPGEnvironment(t *testing.T) {
	e := newEnv(t)
	for k, v := range map[string]string{
		"PGHOST":     "/nonexistent",
		"PGPORT":     "1",
		"PGUSER":     "svn_super",
		"PGPASSWORD": "wrong",
		"PGDATABASE": "postgres",
		"PGSSLMODE":  "verify-full",
		"PGOPTIONS":  "-c statement_timeout=1",
		"PGAPPNAME":  "from-env",
		"PGPASSFILE": "/nonexistent",
	} {
		t.Setenv(k, v)
	}
	db, err := OpenDB(connConfig(e.name, RuntimeRole, runtimePass))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var user, database, timeout, app string
	if err := db.QueryRowContext(e.ctx, `SELECT current_user, current_database(), current_setting('statement_timeout'),
		current_setting('application_name')`).Scan(&user, &database, &timeout, &app); err != nil {
		t.Fatal(err)
	}
	if user != RuntimeRole || database != e.name || timeout != "0" || app != "store-test" {
		t.Fatalf("user=%s database=%s statement_timeout=%s application_name=%s", user, database, timeout, app)
	}
}

// TestErrorsCarryNoValues: a mapped database error keeps its SQLSTATE and
// package error but formats without the DETAIL line, which quotes row
// values.
func TestErrorsCarryNoValues(t *testing.T) {
	e := newEnv(t)
	role, _ := e.callerCreds("p-once", 1)
	err := RegisterCaller(e.ctx, e.owner, "p-twice", 1, role)
	if SQLState(err) != "23505" {
		t.Fatalf("want 23505, got %v", err)
	}
	if strings.Contains(err.Error(), role) {
		t.Fatalf("error text quotes a row value: %v", err)
	}
	db := e.caller("p-cap", 1)
	if _, _, err := sqlEnqueue(e.ctx, db, "k1", `{}`); err != nil {
		t.Fatal(err)
	}
	_, _, err = sqlEnqueue(e.ctx, db, "k2", `{}`)
	wantCode(t, err, "SV005")
	if !errors.Is(mapErr(err), ErrCapReached) || !strings.Contains(mapErr(err).Error(), "SVN_CAP_REACHED") {
		t.Fatalf("SV005 mapped to %v", mapErr(err))
	}
	if mapErr(nil) != nil || SQLState(errors.New("plain")) != "" {
		t.Fatal("nil or plain errors changed")
	}
}

// TestClip: clip drops NUL bytes, replaces invalid UTF-8, and cuts at a rune
// boundary at or below the limit.
func TestClip(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 10, "abc"},
		{"a\x00b", 10, "ab"},
		{"ab\xffc", 10, "ab�c"},
		{"éé", 3, "é"},
		{"abc", 0, ""},
	} {
		got := clip(c.in, c.n)
		if got != c.want || !utf8.ValidString(got) || len(got) > c.n {
			t.Errorf("clip(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
