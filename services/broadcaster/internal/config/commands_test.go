package config

import (
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// TestCallerSQLRole checks the optional SQL-path role of a caller: it is
// registered with the principal, it follows the PostgreSQL name rule, it
// is never the owner or the runtime role, and one role maps one caller.
func TestCallerSQLRole(t *testing.T) {
	m := serveFile()
	m["callers"].([]any)[0].(map[string]any)["sql_role"] = "app_fixture_sql"
	regs := mustParse(t, m).CallerRegistrations()
	if len(regs) != 2 || regs[0] != (CallerRegistration{Principal: "app-fixture", OpenIntentCap: 256, SQLRole: "app_fixture_sql"}) ||
		regs[1].SQLRole != "" {
		t.Fatalf("registrations %+v", regs)
	}
	for name, role := range map[string]string{
		"upper case": "App_SQL", "dash": "app-sql", "leading digit": "1app", "too long": strings.Repeat("a", 64),
		"owner role": store.OwnerRole, "runtime role": store.RuntimeRole,
	} {
		m := serveFile()
		m["callers"].([]any)[0].(map[string]any)["sql_role"] = role
		if _, err := parse(t, m); err == nil || !strings.Contains(err.Error(), "sql_role") {
			t.Errorf("%s: sql_role %q accepted: %v", name, role, err)
		}
	}
	m = serveFile()
	m["callers"].([]any)[0].(map[string]any)["sql_role"] = "shared_sql"
	m["callers"].([]any)[1].(map[string]any)["sql_role"] = "shared_sql"
	if _, err := parse(t, m); err == nil || !strings.Contains(err.Error(), "used twice") {
		t.Fatalf("one role for two callers accepted: %v", err)
	}
}

// TestDatabaseRoleRequirements checks which database role each command
// configuration needs: serve, reconcile-once and signer-clear write the
// outbox as the runtime role; migrate runs as the owner role and names no
// key file; status reads under either role.
func TestDatabaseRoleRequirements(t *testing.T) {
	serve := mustParse(t, serveFile())
	if err := serve.RequireServe(); err != nil {
		t.Fatal(err)
	}
	if err := serve.RequireRuntimeDatabase(); err != nil {
		t.Fatal(err)
	}
	if err := serve.RequireMigrate(); err == nil || !strings.Contains(err.Error(), "owner role") {
		t.Fatalf("migrate under the runtime role accepted: %v", err)
	}
	m := serveFile()
	m["database"].(map[string]any)["role"] = store.OwnerRole
	owner := mustParse(t, m)
	if err := owner.RequireServe(); err == nil || !strings.Contains(err.Error(), "runtime role") {
		t.Fatalf("serve under the owner role accepted: %v", err)
	}
	if err := owner.RequireRuntimeDatabase(); err == nil {
		t.Fatal("runtime database check passed for the owner role")
	}
	if err := owner.RequireMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := owner.RequireDatabase(); err != nil {
		t.Fatal(err)
	}
	m["database"].(map[string]any)["role"] = "cfg_other"
	other := mustParse(t, m)
	if other.RequireMigrate() == nil || other.RequireRuntimeDatabase() == nil {
		t.Fatal("a third role passed the migrate or runtime check")
	}
	if err := other.RequireDatabase(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*Config{mustParse(t, signerFile())} {
		if c.RequireMigrate() == nil || c.RequireRuntimeDatabase() == nil {
			t.Fatal("a configuration without a database passed")
		}
	}
	withKeys := serveFile()
	withKeys["database"].(map[string]any)["role"] = store.OwnerRole
	withKeys["keys"] = signerFile()["keys"]
	if err := mustParse(t, withKeys).RequireMigrate(); err == nil || !strings.Contains(err.Error(), "key files") {
		t.Fatalf("migrate configuration naming key files accepted: %v", err)
	}
}

// TestWorkerNumbers checks the worker's numbers: every PROPOSED value is
// handed over, every key-signed kind has its declared gas (an override
// where configured) and the forward kind has none.
func TestWorkerNumbers(t *testing.T) {
	m := serveFile()
	m["numbers"] = map[string]any{"timeout_offset_blocks": 7, "poll_interval_ms": 250, "stall_window_seconds": 4,
		"withdraw_simulation_margin_percent": 50}
	m["declared_gas_by_kind"] = map[string]int64{"OFFER_LEASE": 300000}
	n, err := mustParse(t, m).WorkerNumbers()
	if err != nil {
		t.Fatal(err)
	}
	if n.TimeoutOffset != 7 || n.PollInterval != 250*time.Millisecond || n.StallWindow != 4*time.Second ||
		n.BackoffBase != time.Second || n.BackoffCap != time.Minute || n.WithdrawMarginPercent != 50 ||
		n.DeadlinePages != 10 || n.AuthContextBytes != 2048 || n.MinLifetime != 30*time.Second ||
		n.MaxLifetime != 24*time.Hour || n.RelayLocalExpiry != time.Hour || n.ForwardWindow != 100 {
		t.Fatalf("worker numbers %+v", n)
	}
	if len(n.DeclaredGas) != 6 || n.DeclaredGas[policy.KindOfferLease] != 300000 ||
		n.DeclaredGas[policy.KindRegisterContent] != 2_000_000 || n.DeclaredGas[policy.KindRelayEndLease] != 2_000_000 {
		t.Fatalf("declared gas %v", n.DeclaredGas)
	}
	if _, ok := n.DeclaredGas[policy.KindForwardProviderTx]; ok {
		t.Fatal("declared gas for the forward kind")
	}
}
