package devnet_test

// The fault proxy of the broadcaster devnet tests: an HTTP reverse proxy on
// loopback between one `svn-broadcaster serve` process and validator 0's
// CometBFT RPC. It reads each bounded JSON-RPC request body, applies the
// rule set for its method (pass, drop the request, forward and drop the
// answer, forward and hold the answer, hold the request, reset the
// connection), and counts per method what it forwarded, dropped and held,
// plus the transaction hash of every broadcast_tx_* body it forwarded.
// Every fault it injects is "fault injected by a test proxy on loopback":
// it shows how the broadcaster handles a lost request or answer on one
// loopback link, not a real network partition. Test reads never go through
// it; they go to the nodes directly.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// bcProxyMode is what the proxy does with one request.
type bcProxyMode int

const (
	// proxyPass forwards the request and returns the node's answer.
	proxyPass bcProxyMode = iota
	// proxyDropRequest closes the client connection without forwarding:
	// the request never reaches the node.
	proxyDropRequest
	// proxyDropResponse forwards the request and closes the client
	// connection without the answer: the node acted, the caller never
	// learns how.
	proxyDropResponse
	// proxyHoldResponse forwards the request and parks the answer until
	// the test releases it (delivered) or discards it (connection closed).
	proxyHoldResponse
	// proxyHold parks the request itself until the test releases it
	// (forwarded then, whether or not the caller still waits) or discards
	// it (never forwarded).
	proxyHold
	// proxyReset resets the client connection without forwarding.
	proxyReset
)

func (m bcProxyMode) String() string {
	return [...]string{"pass", "drop-request", "drop-response", "hold-response", "hold", "reset"}[m]
}

// bcProxyRule applies mode to the next remaining requests of a method;
// remaining < 0 applies it to every request until the rule is cleared.
type bcProxyRule struct {
	mode      bcProxyMode
	remaining int
}

// bcProxyCount counts one method's requests by outcome. AnswerDropped
// counts requests forwarded whose answer was then dropped (they are also
// counted in Forwarded).
type bcProxyCount struct {
	Forwarded     int
	Dropped       int
	Held          int
	Reset         int
	AnswerDropped int
}

// bcHeld is one parked request or answer.
type bcHeld struct {
	method string
	// forwarded is true for a parked answer (proxyHoldResponse): the
	// request already reached the node.
	forwarded bool
	txHash    [32]byte
	decide    chan bool
}

type bcProxy struct {
	name   string
	target string
	ln     net.Listener
	srv    *http.Server
	client *http.Client

	mu          sync.Mutex
	rules       map[string]bcProxyRule
	counts      map[string]*bcProxyCount
	forwardedTx [][32]byte
	held        []*bcHeld
	events      []bcProxyEvent
	// readHold, once armed, holds reads of one transaction: see
	// holdReadsAfterBroadcast.
	readHold *bcReadHold
	closed   bool
}

// bcReadHold is an armed hold of the reads that observe one broadcast: it
// takes the hash of the next forwarded broadcast; from then on a tx lookup
// of that hash, and a tx_search whose query names that hash or contains
// one of selectors, is held. With no selectors every tx and tx_search
// request is held.
type bcReadHold struct {
	selectors []string
	hash      [32]byte
	active    bool
}

// bcProxyEvent is one request the proxy saw, in arrival order: its method,
// what the proxy did, and the transaction hash it names (the broadcast
// transaction's hash, or the hash a tx lookup asks for).
type bcProxyEvent struct {
	at      time.Time
	method  string
	outcome string
	hash    [32]byte
}

// bcProxyBodyCap bounds a request body the proxy reads: a JSON-RPC
// broadcast of the largest transaction (65,536 bytes) in base64 fits.
const bcProxyBodyCap = 1 << 20

// startProxy starts a proxy on a new loopback port in front of target (a
// CometBFT RPC base URL). It stops with the test.
func startProxy(t *testing.T, name, target string) *bcProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &bcProxy{
		name: name, target: strings.TrimRight(target, "/"), ln: ln,
		client: &http.Client{Timeout: 30 * time.Second},
		rules:  map[string]bcProxyRule{}, counts: map[string]*bcProxyCount{},
	}
	p.srv = &http.Server{Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = p.srv.Serve(ln) }()
	t.Cleanup(p.close)
	return p
}

// url is the proxy's base URL, the rpc_url of the serve process behind it.
func (p *bcProxy) url() string { return "http://" + p.ln.Addr().String() }

// close discards every parked request and stops the listener.
func (p *bcProxy) close() {
	p.mu.Lock()
	p.closed = true
	held := p.held
	p.held = nil
	p.mu.Unlock()
	for _, h := range held {
		h.decide <- false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.srv.Shutdown(ctx)
}

// setRule sets the rule of method: mode for the next n requests (n < 0:
// every request until cleared).
func (p *bcProxy) setRule(method string, mode bcProxyMode, n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if mode == proxyPass {
		delete(p.rules, method)
		return
	}
	p.rules[method] = bcProxyRule{mode: mode, remaining: n}
}

// holdReadsAfterBroadcast arms a trigger: once the next broadcast is
// forwarded to the node, and before the caller can see its answer, the
// reads that observe it are held until the rules are cleared: with no
// selectors every tx and tx_search request; otherwise a tx lookup of that
// broadcast's hash and a tx_search whose query names the hash or contains
// a selector. The broadcaster's own effect check before the attempt still
// reads the chain; only its observation of the commit is held.
func (p *bcProxy) holdReadsAfterBroadcast(selectors ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readHold = &bcReadHold{selectors: selectors}
}

// heldRead reports whether an armed read hold covers a request; the
// caller holds p.mu.
func (p *bcProxy) heldRead(method string, hash [32]byte, query string) bool {
	h := p.readHold
	if h == nil || !h.active || len(h.selectors) == 0 {
		return false
	}
	switch method {
	case "tx":
		return hash == h.hash
	case "tx_search":
		if strings.Contains(strings.ToUpper(query), strings.ToUpper(hex.EncodeToString(h.hash[:]))) {
			return true
		}
		for _, s := range h.selectors {
			if strings.Contains(query, s) {
				return true
			}
		}
	}
	return false
}

// clearRules makes every method pass again. Parked requests stay parked
// until released.
func (p *bcProxy) clearRules() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = map[string]bcProxyRule{}
	p.readHold = nil
}

// release decides every parked request or answer of method: forward
// (true) or discard (false). It returns how many it decided.
func (p *bcProxy) release(method string, forward bool) int {
	p.mu.Lock()
	var keep, out []*bcHeld
	for _, h := range p.held {
		if h.method == method {
			out = append(out, h)
		} else {
			keep = append(keep, h)
		}
	}
	p.held = keep
	p.mu.Unlock()
	for _, h := range out {
		h.decide <- forward
	}
	return len(out)
}

// heldTx returns the transaction hashes of the parked broadcast requests
// or answers of method, in arrival order.
func (p *bcProxy) heldTx(method string) [][32]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out [][32]byte
	for _, h := range p.held {
		if h.method == method {
			out = append(out, h.txHash)
		}
	}
	return out
}

// count returns method's counters.
func (p *bcProxy) count(method string) bcProxyCount {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.counts[method]; ok {
		return *c
	}
	return bcProxyCount{}
}

// forwardedTxs returns the hash of every forwarded broadcast transaction,
// in forwarding order.
func (p *bcProxy) forwardedTxs() [][32]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][32]byte(nil), p.forwardedTx...)
}

// summary prints every method's counters on one line, sorted by method.
func (p *bcProxy) summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	methods := make([]string, 0, len(p.counts))
	for m := range p.counts {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	var parts []string
	for _, m := range methods {
		c := p.counts[m]
		parts = append(parts, fmt.Sprintf("%s:fwd=%d,drop=%d,held=%d,reset=%d,answer_drop=%d", m, c.Forwarded, c.Dropped, c.Held, c.Reset,
			c.AnswerDropped))
	}
	return fmt.Sprintf("proxy=%s %s", p.name, strings.Join(parts, " "))
}

// bump updates method's counters and appends the event; the caller holds
// p.mu.
func (p *bcProxy) bump(method, outcome string, hash [32]byte, f func(*bcProxyCount)) {
	c, ok := p.counts[method]
	if !ok {
		c = &bcProxyCount{}
		p.counts[method] = c
	}
	f(c)
	p.events = append(p.events, bcProxyEvent{at: time.Now(), method: method, outcome: outcome, hash: hash})
}

// eventLog returns the events seen so far.
func (p *bcProxy) eventLog() []bcProxyEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bcProxyEvent(nil), p.events...)
}

// rpcRequest is the part of a JSON-RPC request the proxy reads.
type rpcRequest struct {
	Method string `json:"method"`
	Params struct {
		Tx    string `json:"tx"`
		Hash  string `json:"hash"`
		Query string `json:"query"`
	} `json:"params"`
}

func (p *bcProxy) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, bcProxyBodyCap+1))
	if err != nil || len(body) > bcProxyBodyCap {
		http.Error(w, "proxy: request body unreadable or too large", http.StatusRequestEntityTooLarge)
		return
	}
	var req rpcRequest
	method := "(unparsed)"
	if r.Method == http.MethodPost && json.Unmarshal(body, &req) == nil && req.Method != "" {
		method = req.Method
	} else if r.Method == http.MethodGet {
		method = strings.TrimPrefix(r.URL.Path, "/")
	}
	var txHash [32]byte
	if strings.HasPrefix(method, "broadcast_tx_") {
		raw, err := base64.StdEncoding.DecodeString(req.Params.Tx)
		if err != nil {
			http.Error(w, "proxy: broadcast without base64 tx", http.StatusBadRequest)
			return
		}
		txHash = sha256.Sum256(raw)
	} else if method == "tx" {
		if raw, err := base64.StdEncoding.DecodeString(req.Params.Hash); err == nil && len(raw) == 32 {
			copy(txHash[:], raw)
		}
	}

	p.mu.Lock()
	mode := proxyPass
	if !p.closed && p.heldRead(method, txHash, req.Params.Query) {
		mode = proxyHold
	} else if rule, ok := p.rules[method]; ok && !p.closed {
		mode = rule.mode
		if rule.remaining > 0 {
			rule.remaining--
			if rule.remaining == 0 {
				delete(p.rules, method)
			} else {
				p.rules[method] = rule
			}
		}
	}
	var held *bcHeld
	switch mode {
	case proxyDropRequest:
		p.bump(method, "dropped", txHash, func(c *bcProxyCount) { c.Dropped++ })
	case proxyReset:
		p.bump(method, "reset", txHash, func(c *bcProxyCount) { c.Reset++ })
	case proxyHold:
		held = &bcHeld{method: method, txHash: txHash, decide: make(chan bool, 1)}
		p.held = append(p.held, held)
		p.bump(method, "held", txHash, func(c *bcProxyCount) { c.Held++ })
	}
	p.mu.Unlock()

	switch mode {
	case proxyDropRequest, proxyReset:
		hangUp(w, mode == proxyReset)
		return
	case proxyHold:
		if !<-held.decide {
			p.mu.Lock()
			p.bump(method, "discarded", txHash, func(c *bcProxyCount) { c.Dropped++ })
			p.mu.Unlock()
			hangUp(w, false)
			return
		}
	}
	status, answer, err := p.forward(r, body, method, txHash)
	if err != nil {
		http.Error(w, "proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	if strings.HasPrefix(method, "broadcast_tx_") {
		p.mu.Lock()
		if h := p.readHold; h != nil && !h.active {
			h.active, h.hash = true, txHash
			if len(h.selectors) == 0 {
				p.rules["tx"] = bcProxyRule{mode: proxyHold, remaining: -1}
				p.rules["tx_search"] = bcProxyRule{mode: proxyHold, remaining: -1}
			}
		}
		p.mu.Unlock()
	}
	switch mode {
	case proxyDropResponse:
		p.mu.Lock()
		p.bump(method, "answer-dropped", txHash, func(c *bcProxyCount) { c.AnswerDropped++ })
		p.mu.Unlock()
		hangUp(w, false)
		return
	case proxyHoldResponse:
		parked := &bcHeld{method: method, forwarded: true, txHash: txHash, decide: make(chan bool, 1)}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			hangUp(w, false)
			return
		}
		p.held = append(p.held, parked)
		p.bump(method, "answer-held", txHash, func(c *bcProxyCount) { c.Held++ })
		p.mu.Unlock()
		if !<-parked.decide {
			hangUp(w, false)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(answer)
}

// forward sends body to the node and returns its answer, counting the
// forward and the broadcast hash before the request leaves.
func (p *bcProxy) forward(r *http.Request, body []byte, method string, txHash [32]byte) (int, []byte, error) {
	p.mu.Lock()
	p.bump(method, "forwarded", txHash, func(c *bcProxyCount) { c.Forwarded++ })
	if strings.HasPrefix(method, "broadcast_tx_") {
		p.forwardedTx = append(p.forwardedTx, txHash)
	}
	p.mu.Unlock()
	var req *http.Request
	var err error
	if r.Method == http.MethodGet {
		req, err = http.NewRequest(http.MethodGet, p.target+r.URL.RequestURI(), nil)
	} else {
		req, err = http.NewRequest(http.MethodPost, p.target+"/", bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return 0, nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, answer, nil
}

// hangUp closes the client connection without an answer; reset makes the
// close a TCP reset.
func hangUp(w http.ResponseWriter, reset bool) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy: connection cannot be hijacked", http.StatusBadGateway)
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	if tcp, ok := conn.(*net.TCPConn); ok && reset {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}
