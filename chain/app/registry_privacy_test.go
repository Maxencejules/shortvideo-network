package app_test

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// storesHold reports whether any mounted store holds the canary in raw,
// hex, or base64 form.
func storesHold(chainApp *app.App, canary []byte) bool {
	for _, name := range append(app.SnapshotStores(), "acc") {
		if _, found := msgkittest.FindCanary(chainApp.ModuleSnapshot(name), canary); found {
			return true
		}
	}
	return false
}

// TestSubmitterTextFields is the adversarial half of the privacy check. It
// puts the synthetic email and URL into every id field of the six registry
// messages, and into the transaction memo, and asserts the exact outcome.
//
//   - Every id field must match ^[a-z0-9][a-z0-9._-]{0,63}$; an email or a
//     URL never does. Where the handler checks the pattern (registration
//     ids, business ids, the new key id, signature key ids) the result is
//     ERROR_CODE_BOUNDS. Where it looks the id up first (provider_id of
//     UpdateProvider, RotateProviderKey, and DrainProvider; content_id of
//     WithdrawContent) the result is ERROR_CODE_NOT_FOUND, because no
//     record can have an id outside the pattern. Either way the state is
//     byte-identical and no store holds the text afterwards.
//   - An id-shaped value, such as the username canary, does match it. The
//     chain stores it verbatim and the test asserts that it does. The
//     protocol has no field for personal data, but ids are text the
//     submitter chooses; the chain cannot tell a username from an id.
//   - The memo is ERROR_CODE_BOUNDS in the ante handler
//     (TestTransactionsRejectVideoBytes covers the memo, fee payer, fee
//     granter, and tip in full).
//
// A transaction a handler rejects is still in its block: the block data
// keeps its bytes even though state does not change. Only a decode or ante
// rejection keeps a transaction out of the mempool, so out of the blocks an
// honest proposer builds.
func TestSubmitterTextFields(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	wantCode(t, "register c1", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)), msgkit.OK)

	type textCase struct {
		name string
		who  *actor
		text string
		want msgkit.Code
		msg  func() sdk.Msg
	}
	email, url := canaryEmail, canaryEndpoint
	register := func(edit func(m *providersv1.MsgRegisterProvider)) func() sdk.Msg {
		return func() sdk.Msg {
			msg := n.registerProvider(t, n.provider1, "reg-x", "px", groupDevnet, capacity1)
			edit(msg)
			return msg
		}
	}
	update := func(edit func(m *providersv1.MsgUpdateProvider)) func() sdk.Msg {
		return func() sdk.Msg {
			msg := n.updateProvider(t, n.provider1, "upd-x", "p1", capacity1+1, "x")
			edit(msg)
			return msg
		}
	}
	rotate := func(edit func(m *providersv1.MsgRotateProviderKey)) func() sdk.Msg {
		return func() sdk.Msg {
			msg := n.rotate(t, n.provider1, "rot-x", "p1", 1, newSigningKey("p1-next"))
			edit(msg)
			return msg
		}
	}
	drain := func(edit func(m *providersv1.MsgDrainProvider)) func() sdk.Msg {
		return func() sdk.Msg {
			msg := n.drain(t, n.provider1, "drain-x", "p1", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)
			edit(msg)
			return msg
		}
	}
	content := func(edit func(m *contentv1.MsgRegisterContent)) func() sdk.Msg {
		return func() sdk.Msg {
			msg := n.registerContent(t, n.service, "reg-cx", "cx", 1, packageManifest("cx"), sizeC1)
			edit(msg)
			return msg
		}
	}
	withdraw := func(edit func(m *contentv1.MsgWithdrawContent)) func() sdk.Msg {
		return func() sdk.Msg {
			msg := n.withdrawContent(t, n.service, "wd-x", "c1", 1, reasonDigest("x"))
			edit(msg)
			return msg
		}
	}
	var rejected []textCase
	for _, text := range []string{email, url} {
		bounds, notFound := msgkit.Bounds, msgkit.NotFound
		rejected = append(rejected,
			textCase{"RegisterProvider business_id", n.provider1, text, bounds, register(func(m *providersv1.MsgRegisterProvider) { m.BusinessId = text })},
			textCase{"RegisterProvider provider_id", n.provider1, text, bounds, register(func(m *providersv1.MsgRegisterProvider) { m.ProviderId = text })},
			textCase{"RegisterProvider signing_key_id", n.provider1, text, bounds, register(func(m *providersv1.MsgRegisterProvider) { m.SigningKeyId = text })},
			textCase{"RegisterProvider operator_group_id", n.provider1, text, bounds, register(func(m *providersv1.MsgRegisterProvider) { m.OperatorGroupId = text })},
			textCase{"UpdateProvider business_id", n.provider1, text, bounds, update(func(m *providersv1.MsgUpdateProvider) { m.BusinessId = text })},
			textCase{"UpdateProvider provider_id", n.provider1, text, notFound, update(func(m *providersv1.MsgUpdateProvider) { m.ProviderId = text })},
			textCase{"RotateProviderKey business_id", n.provider1, text, bounds, rotate(func(m *providersv1.MsgRotateProviderKey) { m.BusinessId = text })},
			textCase{"RotateProviderKey provider_id", n.provider1, text, notFound, rotate(func(m *providersv1.MsgRotateProviderKey) { m.ProviderId = text })},
			textCase{"RotateProviderKey new_signing_key_id", n.provider1, text, bounds, func() sdk.Msg {
				next := newSigningKey("p1-next")
				next.id = text
				return n.rotate(t, n.provider1, "rot-x", "p1", 1, next)
			}},
			textCase{"RotateProviderKey current_key_signature key_id", n.provider1, text, bounds, rotate(func(m *providersv1.MsgRotateProviderKey) { m.CurrentKeySignature.KeyId = text })},
			textCase{"DrainProvider business_id", n.provider1, text, bounds, drain(func(m *providersv1.MsgDrainProvider) { m.BusinessId = text })},
			textCase{"DrainProvider provider_id", n.provider1, text, notFound, drain(func(m *providersv1.MsgDrainProvider) { m.ProviderId = text })},
			textCase{"DrainProvider provider_signature key_id", n.provider1, text, bounds, drain(func(m *providersv1.MsgDrainProvider) { m.ProviderSignature.KeyId = text })},
			textCase{"RegisterContent business_id", n.service, text, bounds, content(func(m *contentv1.MsgRegisterContent) { m.BusinessId = text })},
			textCase{"RegisterContent content_id", n.service, text, bounds, content(func(m *contentv1.MsgRegisterContent) { m.ContentId = text })},
			textCase{"RegisterContent hosting_intent key_id", n.service, text, bounds, content(func(m *contentv1.MsgRegisterContent) { m.HostingIntent.KeyId = text })},
			textCase{"WithdrawContent business_id", n.service, text, bounds, withdraw(func(m *contentv1.MsgWithdrawContent) { m.BusinessId = text })},
			// The intent is signed over the text, so the lookup is the
			// first check the text fails.
			textCase{"WithdrawContent content_id", n.service, text, notFound, func() sdk.Msg {
				return n.withdrawContent(t, n.service, "wd-x", text, 1, reasonDigest("x"))
			}},
			textCase{"WithdrawContent hosting_intent key_id", n.service, text, bounds, withdraw(func(m *contentv1.MsgWithdrawContent) { m.HostingIntent.KeyId = text })},
		)
	}
	if len(rejected) != 2*19 {
		t.Fatalf("%d cases; every id field of the six messages is listed once per text", len(rejected))
	}
	for _, tc := range rejected {
		raw := n.signed(t, c, tc.who, registryGas, nil, tc.msg())
		if !bytes.Contains(raw, []byte(tc.text)) {
			t.Fatalf("%s: the text is not in the signed bytes", tc.name)
		}
		before := chainApp.FullSnapshot()
		res := n.deliverRaw(t, c, tc.who, raw)
		wantCode(t, tc.name+" = "+tc.text, res, tc.want)
		unchanged(t, tc.name, chainApp, before, res)
		if storesHold(chainApp, []byte(tc.text)) {
			t.Fatalf("%s: a store holds %q after the rejection", tc.name, tc.text)
		}
	}

	// The memo never reaches a handler.
	before := chainApp.FullSnapshot()
	memo := n.signEnvelope(t, n.service, n.registerContent(t, n.service, "reg-memo", "memo", 1, packageManifest("memo"), sizeC1), func(body *txtypes.TxBody, _ *txtypes.AuthInfo) {
		body.Memo = email
	}, nil)
	res := n.deliverRaw(t, c, n.service, memo)
	wantCode(t, "memo", res, msgkit.Bounds)
	if anteCommitted(res) {
		t.Fatal("a memo passed the ante handler")
	}
	unchanged(t, "memo", chainApp, before, res)

	// An id-shaped value is accepted and stored verbatim.
	user := []byte(canaryUser)
	if storesHold(chainApp, user) {
		t.Fatal("the username canary is in a store before it was submitted")
	}
	wantCode(t, "content_id = username", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-u1", canaryUser, 1, packageManifest("u1"), sizeC1)), msgkit.OK)
	if _, found := msgkittest.FindCanary(chainApp.ModuleSnapshot(app.ModuleContent), user); !found {
		t.Fatal("an id-shaped username was not stored verbatim; update the documented limit")
	}
	wantCode(t, "operator_group_id = username", n.send(t, c, n.provider2, n.registerProvider(t, n.provider2, "reg-u2", "pu", canaryUser, capacity1)), msgkit.OK)
	if _, found := msgkittest.FindCanary(chainApp.ModuleSnapshot(app.ModuleProviders), user); !found {
		t.Fatal("an id-shaped group id was not stored verbatim; update the documented limit")
	}
}
