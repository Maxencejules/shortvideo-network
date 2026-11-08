package worker

import (
	"encoding/json"
	"testing"
	"time"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// TestValidationReadsAtTheApplicationFloor (findings D1/F1 of the goal 11
// flake review): CometBFT's /status reports a block before the application
// has committed it, so while block H executes every ABCI query answers at
// H-1 (chain.Status.AppFloor). The fake node answers every query one block
// below its /status height. The worker's checks must read at that floor:
// the check before signing of a register, an offer, a relay acceptance and
// a forward passes and the attempt is sent in the same pass, and the
// SQL-path validation marks the intent validated. A forward's window still
// counts from the /status height. A node answering two blocks below its
// /status height is still stale: the intent waits, unsigned.
func TestValidationReadsAtTheApplicationFloor(t *testing.T) {
	t.Run("register", func(t *testing.T) {
		e := newEnv(t)
		e.node.queryLag = 1
		id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
		e.pass(e.session(policy.ClassService))
		e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
		if n := len(e.attempts(id)); n != 1 || e.logged("below the needed") {
			t.Fatalf("%d attempts; worker log:\n%s", n, e.logs.String())
		}
	})
	t.Run("offer", func(t *testing.T) {
		e := newEnv(t)
		e.node.queryLag = 1
		s := e.session(policy.ClassService)
		reg := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
		e.pass(s)
		e.node.block()
		e.pass(s)
		e.wantState(reg, "COMMITTED", "", "")
		if err := e.w.housekeepingPass(e.ctx); err != nil || e.ownerStatus(reg) != "EFFECTIVE" {
			t.Fatalf("owner row %s after housekeeping (%v), want EFFECTIVE", e.ownerStatus(reg), err)
		}
		id := e.enqueue(principalApp, "offer-1", policy.KindOfferLease, offerMsg("o1", "c1"), time.Hour)
		e.pass(s)
		e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
		if n := len(e.attempts(id)); n != 1 || e.logged("below the needed") {
			t.Fatalf("%d attempts; worker log:\n%s", n, e.logs.String())
		}
	})
	t.Run("relay acceptance", func(t *testing.T) {
		e := newEnv(t)
		e.node.queryLag = 1
		p := e.addProvider()
		id := e.enqueue(principalApp, "accept-1", policy.KindRelayAcceptLease, &leasesv1.MsgAcceptLease{OfferId: "o1", LeaseId: "l1",
			ProviderAcceptance: e.acceptance(p, "l1")}, time.Hour)
		e.pass(e.session(policy.ClassBroadcaster))
		e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
		if n := len(e.attempts(id)); n != 1 || e.logged("below the needed") {
			t.Fatalf("%d attempts; worker log:\n%s", n, e.logs.String())
		}
	})
	t.Run("forward", func(t *testing.T) {
		e := newEnv(t)
		e.node.queryLag = 1
		p := e.addProvider()
		msg := &leasesv1.MsgAcceptLease{Signer: p.account.PubKey().Address(), BusinessId: "provider-accept-1", OfferId: "o1", LeaseId: "l1",
			ProviderAcceptance: e.acceptance(p, "l1")}
		timeout := uint64(e.node.heightNow()) + 50
		id := e.enqueueForward(p, "forward-1", e.providerTx(p, msg, 0, timeout), msg, 0, timeout)
		e.pass(e.forwarder())
		e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
		if n := len(e.attempts(id)); n != 1 || e.logged("below the needed") {
			t.Fatalf("%d attempts; worker log:\n%s", n, e.logs.String())
		}
	})
	t.Run("forward window counts from the status height", func(t *testing.T) {
		// A timeout height equal to the /status height is refused: the
		// window starts above the latest block the node reported, not
		// above the read floor.
		e := newEnv(t)
		e.node.queryLag = 1
		p := e.addProvider()
		msg := &leasesv1.MsgAcceptLease{Signer: p.account.PubKey().Address(), BusinessId: "provider-accept-1", OfferId: "o1", LeaseId: "l1",
			ProviderAcceptance: e.acceptance(p, "l1")}
		timeout := uint64(e.node.heightNow())
		id := e.enqueueForward(p, "forward-1", e.providerTx(p, msg, 0, timeout), msg, 0, timeout)
		e.pass(e.forwarder())
		e.wantState(id, "FAILED", "", "INVALID")
		if n := len(e.attempts(id)); n != 0 || e.node.broadcastCount() != 0 {
			t.Fatalf("%d attempts, %d broadcasts for a forward whose timeout height is the status height", n, e.node.broadcastCount())
		}
	})
	t.Run("SQL-path validation", func(t *testing.T) {
		e := newEnv(t)
		e.node.queryLag = 1
		authJSON, err := json.Marshal(fullAuth())
		if err != nil {
			t.Fatal(err)
		}
		id := sqlEnqueue(t, e.sqlCaller(principalSQL), "REGISTER_CONTENT", registerJSON(e.registerMsg("c1")), string(authJSON), "sql-1")
		if err := e.w.housekeepingPass(e.ctx); err != nil {
			t.Fatal(err)
		}
		work, err := e.st.GetWork(e.ctx, id)
		if err != nil || !work.Validated || work.BusinessID == "" {
			t.Fatalf("SQL-path intent after validation: %+v %v; worker log:\n%s", work, err, e.logs.String())
		}
	})
	t.Run("two blocks behind is stale", func(t *testing.T) {
		e := newEnv(t)
		e.node.queryLag = 2
		id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
		e.pass(e.session(policy.ClassService))
		e.wantState(id, "REQUESTED", "QUEUED", "")
		if n := len(e.attempts(id)); n != 0 || e.signer.callCount() != 0 || !e.logged("answered at height 8, below the needed 9") {
			t.Fatalf("%d attempts, %d signatures; worker log:\n%s", n, e.signer.callCount(), e.logs.String())
		}
	})
}
