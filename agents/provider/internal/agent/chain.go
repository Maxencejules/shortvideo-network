package agent

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/sign"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
)

var errChain = errors.New("agent: chain view")

// chainView is what the operator's HTTPS endpoint reported. It is not a
// light-client proof. A broadcast acknowledgement is not part of this view.
type chainView struct {
	Height   uint64
	Account  uint64
	Sequence uint64
	Catching bool
}

func (a *Agent) poll() (chainView, error) {
	lim := a.chainLimit()
	var view chainView
	body, err := fetch.Bytes(a.ctx, a.cfg.ChainURL+"/status", lim)
	if err != nil {
		return view, err
	}
	var status struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
				CatchingUp        bool   `json:"catching_up"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &status) != nil {
		return view, errChain
	}
	height, err := strconv.ParseUint(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil || height == 0 {
		return view, errChain
	}
	view.Height = height
	view.Catching = status.Result.SyncInfo.CatchingUp
	acc, err := fetch.Bytes(a.ctx, a.cfg.ChainURL+"/account", lim)
	if err != nil {
		return view, err
	}
	var account struct {
		Account  string `json:"account_number"`
		Sequence string `json:"sequence"`
	}
	if json.Unmarshal(acc, &account) != nil {
		return view, errChain
	}
	view.Account, err = strconv.ParseUint(account.Account, 10, 64)
	if err != nil {
		return view, err
	}
	view.Sequence, err = strconv.ParseUint(account.Sequence, 10, 64)
	return view, err
}

func (a *Agent) pollLease(id string) (string, error) {
	body, err := fetch.Bytes(a.ctx, a.cfg.ChainURL+"/lease/"+id, a.chainLimit())
	if err != nil {
		return "", err
	}
	var doc struct {
		State string `json:"state"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return "", errChain
	}
	return doc.State, nil
}

func (a *Agent) broadcast(raw []byte) error {
	lim := a.chainLimit()
	lim.MaxBytes = 1 << 20
	_, err := fetch.Post(a.ctx, a.cfg.ChainURL+"/broadcast", raw, lim)
	return err
}

func (a *Agent) chainLimit() fetch.Limit {
	return fetch.Limit{
		MaxBytes: 1 << 20,
		Timeout:  5 * time.Second,
		CA:       a.chainC,
		Allow:    a.cfg.ChainDestinations,
	}
}

func (a *Agent) apply(view chainView, err error) {
	if err != nil || view.Catching || view.Height < a.st.ChainHeight {
		a.st.Uncertain = true
		return
	}
	now := time.Now()
	if view.Height > a.st.ChainHeight {
		a.st.ChainHeight = view.Height
		a.st.ChainHeightUnix = now.Unix()
		a.heightAt = now
		a.st.Uncertain = false
		return
	}
	if a.heightAt.IsZero() || now.Sub(a.heightAt) >= time.Duration(a.cfg.StallAfterSeconds)*time.Second {
		a.st.Uncertain = true
		return
	}
	a.st.Uncertain = false
}

func (a *Agent) finish(lease *store.Lease) {
	view, err := a.poll()
	state := ""
	if err == nil {
		state, _ = a.pollLease(lease.LeaseID)
	}
	a.mu.Lock()
	a.apply(view, err)
	a.advance(lease, state)
	_ = a.save()
	a.mu.Unlock()
}

func (a *Agent) refresh() {
	view, err := a.poll()
	a.mu.Lock()
	ids := make([]string, 0, len(a.st.Leases))
	for id := range a.st.Leases {
		ids = append(ids, id)
	}
	a.apply(view, err)
	a.mu.Unlock()
	states := map[string]string{}
	if err == nil {
		for _, id := range ids {
			state, serr := a.pollLease(id)
			if serr == nil {
				states[id] = state
			}
		}
	}
	a.mu.Lock()
	for id, lease := range a.st.Leases {
		a.advance(lease, states[id])
	}
	_ = a.save()
	a.mu.Unlock()
}

func (a *Agent) advance(lease *store.Lease, state string) {
	if lease.Readiness == store.Removed || lease.Readiness == store.Expired || lease.Readiness == store.Transferring || lease.Readiness == store.Staged {
		return
	}
	now := time.Now().Unix()
	if now >= lease.Deadline {
		a.localEnd(lease, store.Expired)
		return
	}
	if a.st.Uncertain {
		return
	}
	switch state {
	case "ENDED", "EXPIRED":
		a.localEnd(lease, store.Expired)
	case "ACTIVE", "RENEWED":
		if lease.Deadline-now <= a.cfg.ClockUncertaintySeconds {
			lease.Readiness = store.Committed
			return
		}
		lease.Readiness = store.Serving
	}
}

func (a *Agent) localEnd(lease *store.Lease, readiness string) {
	if lease.Readiness == store.Removed || lease.Readiness == store.Expired {
		return
	}
	if a.st.KeyID != "" {
		if sig, _, err := sign.End(a.key.Private(), a.st.KeyID, lease.LeaseID, a.cfg.ProviderID, lease.TermVersion); err == nil {
			a.st.Ends = append(a.st.Ends, hex.EncodeToString(sig.GetSignature()))
		}
	}
	removePackage(a.cfg.DataDir, lease)
	a.release(lease.PackageBytes)
	lease.Readiness = readiness
	lease.ServeToken = ""
}

func removePackage(dir string, lease *store.Lease) {
	os.RemoveAll(packageDir(dir, lease))
	os.RemoveAll(filepath.Join(dir, "staging", lease.LeaseID))
}
