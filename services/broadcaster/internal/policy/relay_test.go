package policy

import (
	"context"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func wantCode(t *testing.T, err error, want Code) {
	t.Helper()
	if got := CodeOf(err); got != want {
		t.Fatalf("refusal = %v (code %q), want code %q", err, got, want)
	}
}

// TestProviderPrecheck covers plan 13.1: a relayed acceptance, renewal or
// end verifies only against the provider's current registered key. The
// service key, the broadcaster key, another provider's key, the
// provider's retired key, a random key, the wrong domain and the wrong
// lease id are each refused with no read of anything but chain state.
func TestProviderPrecheck(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	c := w.chain.client(t)

	t.Run("valid relays pass", func(t *testing.T) {
		res, err := PrecheckRelay(ctx, c, w.relayInput(w.acceptMsg(t, w.p1, worldLeaseID)))
		if err != nil {
			t.Fatal(err)
		}
		if res.ProviderID != "provider-1" || res.OfferID != worldOfferID || res.LeaseID != worldLeaseID || res.KeyGeneration != 2 {
			t.Fatalf("accept result = %+v", res)
		}
		for _, msg := range []sdkMsg{w.renewMsg(t, w.p1), w.endMsg(t, w.p1, 1)} {
			res, err := PrecheckRelay(ctx, c, w.relayInput(msg))
			if err != nil {
				t.Fatalf("%T: %v", msg, err)
			}
			if res.ProviderID != "provider-1" || res.OfferID != "offer-0" || res.LeaseID != worldHeldLease {
				t.Fatalf("%T result = %+v", msg, res)
			}
		}
	})

	forgers := map[string]innerKey{
		"service key":          w.service,
		"broadcaster key":      w.broadcasterKey,
		"other provider's key": w.p2,
		"retired key":          w.p1Old,
		"random key":           w.random,
	}
	for name, k := range forgers {
		t.Run(name, func(t *testing.T) {
			for _, msg := range []sdkMsg{w.acceptMsg(t, k, worldLeaseID), w.renewMsg(t, k), w.endMsg(t, k, 1)} {
				_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
				wantCode(t, err, CodeProviderSignature)
			}
		})
		// The same forged signature carrying the provider's current key id
		// and public key still fails: the ed25519 check, not the labels.
		t.Run(name+" with the registered key labels", func(t *testing.T) {
			for _, msg := range []sdkMsg{w.acceptMsg(t, k, worldLeaseID), w.renewMsg(t, k), w.endMsg(t, k, 1)} {
				sig := innerOf(msg)
				sig.PublicKey, sig.KeyId = append([]byte(nil), w.p1.pub...), w.p1.id
				_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
				wantCode(t, err, CodeProviderSignature)
			}
		})
	}

	t.Run("wrong domain", func(t *testing.T) {
		sb, err := leases.AcceptSignBytes(w.offer, worldLeaseID)
		if err != nil {
			t.Fatal(err)
		}
		msg := w.acceptMsg(t, w.p1, worldLeaseID)
		msg.ProviderAcceptance = w.p1.innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, sb)
		_, err = PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeProviderSignature)
		end := w.endMsg(t, w.p1, 1)
		end.ProviderSignature.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT
		_, err = PrecheckRelay(ctx, c, w.relayInput(end))
		wantCode(t, err, CodeProviderSignature)
	})
	t.Run("wrong lease id", func(t *testing.T) {
		msg := w.acceptMsg(t, w.p1, "lease-other")
		msg.LeaseId = worldLeaseID
		_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeProviderSignature)
	})
	t.Run("wrong key id", func(t *testing.T) {
		msg := w.acceptMsg(t, w.p1, worldLeaseID)
		msg.ProviderAcceptance.KeyId = w.p1Old.id
		_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeProviderSignature)
	})
	t.Run("end signed for an earlier term", func(t *testing.T) {
		w.lease.TermVersion = 2
		defer func() { w.lease.TermVersion = 1 }()
		_, err := PrecheckRelay(ctx, c, w.relayInput(w.endMsg(t, w.p1, 1)))
		wantCode(t, err, CodeProviderSignature)
		if _, err := PrecheckRelay(ctx, c, w.relayInput(w.endMsg(t, w.p1, 2))); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("renew field changed after signing", func(t *testing.T) {
		msg := w.renewMsg(t, w.p1)
		msg.DeadlineUnixSeconds++
		_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeProviderSignature)
	})
	t.Run("missing signature", func(t *testing.T) {
		msg := w.acceptMsg(t, w.p1, worldLeaseID)
		msg.ProviderAcceptance = nil
		_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeProviderSignature)
	})
	t.Run("authority end", func(t *testing.T) {
		msg := w.endMsg(t, w.p1, 1)
		msg.AuthorityEnd = true
		_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeAuthorityEnd)
	})
	t.Run("offer not found", func(t *testing.T) {
		msg := w.acceptMsg(t, w.p1, worldLeaseID)
		msg.OfferId = "offer-absent"
		_, err := PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeOfferNotFound)
	})
	t.Run("offer beyond the page budget is not found, never skipped", func(t *testing.T) {
		w.chain.pageSize = 2
		defer func() { w.chain.pageSize = 100 }()
		in := w.relayInput(w.acceptMsg(t, w.p1, worldLeaseID))
		in.DeadlinePages = 3 // six offers read; the offer is the sixth
		if _, err := PrecheckRelay(ctx, c, in); err != nil {
			t.Fatalf("offer within the budget: %v", err)
		}
		in.DeadlinePages = 2
		_, err := PrecheckRelay(ctx, c, in)
		wantCode(t, err, CodeOfferNotFound)
	})
	t.Run("deadline page budget bounds", func(t *testing.T) {
		in := w.relayInput(w.acceptMsg(t, w.p1, worldLeaseID))
		for _, pages := range []int{0, MaxDeadlinePages + 1} {
			in.DeadlinePages = pages
			if _, err := PrecheckRelay(ctx, c, in); err == nil || CodeOf(err) != "" {
				t.Fatalf("budget %d: %v", pages, err)
			}
		}
	})
	t.Run("offer of another provider or content than named", func(t *testing.T) {
		in := w.relayInput(w.acceptMsg(t, w.p1, worldLeaseID))
		in.ProviderID = "provider-2"
		_, err := PrecheckRelay(ctx, c, in)
		wantCode(t, err, CodeOfferNotFound)
		in.ProviderID, in.ContentID = "", worldContent2
		_, err = PrecheckRelay(ctx, c, in)
		wantCode(t, err, CodeOfferNotFound)
	})
	t.Run("offer expired at chain time", func(t *testing.T) {
		in := w.relayInput(w.acceptMsg(t, w.p1, worldLeaseID))
		in.ChainTime = in.ChainTime.Add(3600e9)
		_, err := PrecheckRelay(ctx, c, in)
		wantCode(t, err, CodeOfferExpired)
	})
	t.Run("provider not admitted refuses an accept only", func(t *testing.T) {
		w.chain.providers["provider-1"].State = providersv1.ProviderState_PROVIDER_STATE_DRAINING
		defer func() { w.chain.providers["provider-1"].State = providersv1.ProviderState_PROVIDER_STATE_ADMITTED }()
		_, err := PrecheckRelay(ctx, c, w.relayInput(w.acceptMsg(t, w.p1, worldLeaseID)))
		wantCode(t, err, CodeProviderNotAdmitted)
		for _, msg := range []sdkMsg{w.renewMsg(t, w.p1), w.endMsg(t, w.p1, 1)} {
			if _, err := PrecheckRelay(ctx, c, w.relayInput(msg)); err != nil {
				t.Fatalf("%T for a draining provider: %v", msg, err)
			}
		}
	})
	t.Run("provider missing", func(t *testing.T) {
		saved := w.chain.providers["provider-1"]
		delete(w.chain.providers, "provider-1")
		defer func() { w.chain.providers["provider-1"] = saved }()
		_, err := PrecheckRelay(ctx, c, w.relayInput(w.renewMsg(t, w.p1)))
		wantCode(t, err, CodeProviderNotFound)
	})
	t.Run("lease not held by the named pair", func(t *testing.T) {
		in := w.relayInput(w.renewMsg(t, w.p1))
		in.ContentID = worldContent
		_, err := PrecheckRelay(ctx, c, in)
		wantCode(t, err, CodeLeaseNotFound)
		msg := w.endMsg(t, w.p1, 1)
		msg.LeaseId = "lease-other"
		_, err = PrecheckRelay(ctx, c, w.relayInput(msg))
		wantCode(t, err, CodeLeaseNotFound)
	})
	t.Run("read failure is not a refusal", func(t *testing.T) {
		w.chain.failPath = "/svn.providers.v1.Query/Provider"
		defer func() { w.chain.failPath = "" }()
		_, err := PrecheckRelay(ctx, c, w.relayInput(w.renewMsg(t, w.p1)))
		if err == nil || CodeOf(err) != "" {
			t.Fatalf("read failure = %v, want a plain error", err)
		}
	})
	t.Run("stale node is not a refusal", func(t *testing.T) {
		in := w.relayInput(w.renewMsg(t, w.p1))
		in.MinHeight = w.chain.height + 1
		_, err := PrecheckRelay(ctx, c, in)
		if err == nil || CodeOf(err) != "" {
			t.Fatalf("stale read = %v, want a plain error", err)
		}
	})
	t.Run("not a relay message", func(t *testing.T) {
		_, err := PrecheckRelay(ctx, c, RelayInput{Msg: &leasesv1.MsgOfferLease{}})
		wantCode(t, err, CodeInvalid)
	})
}

// innerOf returns the inner signature of a relay message.
func innerOf(msg sdkMsg) *typesv1.InnerSignature {
	switch m := msg.(type) {
	case *leasesv1.MsgAcceptLease:
		return m.GetProviderAcceptance()
	case *leasesv1.MsgRenewLease:
		return m.GetProviderSignature()
	case *leasesv1.MsgEndLease:
		return m.GetProviderSignature()
	}
	return nil
}
