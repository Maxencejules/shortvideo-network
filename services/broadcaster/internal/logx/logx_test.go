package logx

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// canary returns a fresh run-time string that appears nowhere else.
func canary(t *testing.T) string {
	t.Helper()
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return "canary" + hex.EncodeToString(b[:])
}

// decode returns the one JSON record in buf.
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log output is not one JSON record: %v: %q", err, buf.String())
	}
	return rec
}

// TestRedactorDropsSecrets logs secrets under the names and types the
// broadcaster could carry them in and checks that none reaches the output,
// while ids, hashes and plain numbers stay readable.
func TestRedactorDropsSecrets(t *testing.T) {
	secretNames := []string{
		"key", "signing_key", "keyMaterial", "key_file", "public_key", "seed", "mnemonic", "secret", "client_secret",
		"password", "db_passwd", "passfile", "dsn", "conninfo", "credentials", "token", "page_token", "cookie",
		"Authorization", "auth_context", "authContext", "cert", "tls_certificate", "signature",
		"provider_signature", "sig", "acceptance", "provider_acceptance", "hosting_intent", "tx_bytes", "txBytes",
		"raw_tx", "tx", "raw", "data", "bytes", "body", "request_body", "payload", "request", "response",
		"preimage", "sign_doc", "private_thing",
	}
	for _, name := range secretNames {
		t.Run(name, func(t *testing.T) {
			secret := canary(t)
			var buf bytes.Buffer
			New(&buf, slog.LevelDebug).Info("event", name, secret)
			if strings.Contains(buf.String(), secret) {
				t.Fatalf("attribute %q leaked its value: %s", name, buf.String())
			}
			if got := decode(t, &buf)[name]; got != Redacted {
				t.Fatalf("attribute %q = %v, want %q", name, got, Redacted)
			}
		})
	}

	t.Run("bytes under any name", func(t *testing.T) {
		secret := canary(t)
		var buf bytes.Buffer
		New(&buf, slog.LevelDebug).Info("event", "detail", []byte(secret), "count", 3)
		if strings.Contains(buf.String(), secret) || strings.Contains(buf.String(), hex.EncodeToString([]byte(secret))) {
			t.Fatalf("byte value leaked: %s", buf.String())
		}
		rec := decode(t, &buf)
		if rec["detail"] != Redacted || rec["count"] != float64(3) {
			t.Fatalf("record = %v", rec)
		}
	})

	t.Run("struct map and slice values", func(t *testing.T) {
		secret := canary(t)
		type carrier struct{ Field string }
		var buf bytes.Buffer
		New(&buf, slog.LevelDebug).Info("event",
			"thing", carrier{Field: secret},
			"set", map[string]string{"a": secret},
			"list", []string{secret})
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("composite value leaked: %s", buf.String())
		}
	})

	t.Run("denied group redacts its members", func(t *testing.T) {
		secret := canary(t)
		var buf bytes.Buffer
		New(&buf, slog.LevelDebug).Info("event", slog.Group("auth_context", slog.String("principal_id", secret)))
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("grouped value leaked: %s", buf.String())
		}
		logger := New(&buf, slog.LevelDebug).WithGroup("credentials")
		buf.Reset()
		logger.Info("event", "name", secret)
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("WithGroup value leaked: %s", buf.String())
		}
	})

	t.Run("long strings and messages", func(t *testing.T) {
		secret := canary(t)
		long := strings.Repeat(secret, MaxStringBytes/len(secret)+1)
		var buf bytes.Buffer
		New(&buf, slog.LevelDebug).Info(long, "detail", long)
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("long string leaked: %s", buf.String())
		}
		rec := decode(t, &buf)
		if rec[slog.MessageKey] != Redacted || rec["detail"] != Redacted {
			t.Fatalf("record = %v", rec)
		}
	})

	t.Run("kept values", func(t *testing.T) {
		var buf bytes.Buffer
		hash := strings.Repeat("ab", 32)
		New(&buf, slog.LevelDebug).Info("signed",
			"intent_id", "3f9c1a52-0000-4000-8000-000000000001", "signer", "service", "tx_hash", hash,
			"type_url", "/svn.leases.v1.MsgOfferLease", "height", int64(42), "sequence", uint64(7),
			"ok", true, "wait", 2*time.Second, "err", errors.New("chain: status: refused"))
		rec := decode(t, &buf)
		for k, want := range map[string]any{
			"intent_id": "3f9c1a52-0000-4000-8000-000000000001", "signer": "service", "tx_hash": hash,
			"type_url": "/svn.leases.v1.MsgOfferLease", "height": float64(42), "sequence": float64(7), "ok": true,
			"err": "chain: status: refused", slog.MessageKey: "signed",
		} {
			if rec[k] != want {
				t.Errorf("%s = %v, want %v", k, rec[k], want)
			}
		}
		if rec["wait"] == Redacted {
			t.Errorf("duration was redacted")
		}
	})

	t.Run("long error text is cut", func(t *testing.T) {
		var buf bytes.Buffer
		New(&buf, slog.LevelDebug).Info("event", "err", errors.New(strings.Repeat("e", 2*MaxErrorBytes)))
		if got, _ := decode(t, &buf)["err"].(string); len(got) != MaxErrorBytes {
			t.Fatalf("error text length %d, want %d", len(got), MaxErrorBytes)
		}
	})
}

// requestBody is the shape of a caller's create request (plan 5.3): a
// struct, so the JSON handler would marshal every field if the hook let it
// through.
type requestBody struct {
	Kind           string `json:"kind"`
	IdempotencyKey string `json:"idempotency_key"`
	ContentID      string `json:"content_id"`
	Signature      []byte `json:"signature"`
}

// TestRedactorDropsRequestBody logs a caller's request body the ways a
// handler bug could (as the decoded struct, as raw JSON bytes, as a string,
// and through a LogValuer) and checks that no field value reaches the
// output. It kills the "log the request body" mutant at the slog level.
func TestRedactorDropsRequestBody(t *testing.T) {
	secret := canary(t)
	body := requestBody{Kind: "REGISTER_CONTENT", IdempotencyKey: secret + "k", ContentID: secret + "c", Signature: []byte(secret)}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]any{
		"struct":           {"request", body},
		"struct innocent":  {"detail", body},
		"pointer":          {"detail", &body},
		"raw json bytes":   {"detail", raw},
		"raw json string":  {"detail", string(raw)},
		"json.RawMessage":  {"detail", json.RawMessage(raw)},
		"log valuer":       {"detail", valuer{body: string(raw)}},
		"body as group":    {slog.Group("body", slog.String("content_id", body.ContentID))},
		"request id field": {"request", body.ContentID},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			New(&buf, slog.LevelDebug).Info("received", args...)
			if strings.Contains(buf.String(), secret) {
				t.Fatalf("request body leaked: %s", buf.String())
			}
			if !strings.Contains(buf.String(), Redacted) {
				t.Fatalf("no redaction marker, so the attribute vanished or was logged: %s", buf.String())
			}
		})
	}
}

// valuer resolves to a long string, as a LogValuer wrapping a body would.
type valuer struct{ body string }

func (v valuer) LogValue() slog.Value {
	return slog.StringValue(v.body + strings.Repeat(" ", MaxStringBytes))
}

func TestDeniedNames(t *testing.T) {
	for name, want := range map[string]bool{
		"signer": false, "signer_name": false, "intent_id": false, "tx_hash": false, "height": false,
		"request_id": false, "sig": true, "SIG": true, "signature_hex": true, "Hosting-Intent": true,
		"idempotency_key": true, "x.private.y": true,
	} {
		if got := Denied(name); got != want {
			t.Errorf("Denied(%q) = %v, want %v", name, got, want)
		}
	}
}
