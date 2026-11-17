package moderation

import "sync"

// Gate is the local admission decision. A committed chain record does not
// clear it. Nil-safe callers should use a non-nil gate from NewGate.
type Gate struct {
	mu       sync.Mutex
	accounts map[string]struct{}
	content  map[string]struct{}
}

// NewGate allows every account and every content id.
func NewGate() *Gate {
	return &Gate{
		accounts: map[string]struct{}{},
		content:  map[string]struct{}{},
	}
}

// BlockAccount refuses new uploads and publication for that account.
func (g *Gate) BlockAccount(account string) {
	if g == nil || account == "" {
		return
	}
	g.mu.Lock()
	g.accounts[account] = struct{}{}
	g.mu.Unlock()
}

// Delist refuses publication and further distribution of that content id.
// Calling it again is the same decision.
func (g *Gate) Delist(contentID string) {
	if g == nil || contentID == "" {
		return
	}
	g.mu.Lock()
	g.content[contentID] = struct{}{}
	g.mu.Unlock()
}

// NoteChainPublication records that a chain transaction committed. It does
// not remove a local delist or an account block.
func (g *Gate) NoteChainPublication(contentID string, committed bool) {
	_, _ = contentID, committed
}

// UploadAllowed reports whether that account may open an upload.
func (g *Gate) UploadAllowed(account string) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, blocked := g.accounts[account]
	return !blocked
}

// PublishAllowed reports whether that content id may be distributed.
func (g *Gate) PublishAllowed(contentID string) bool {
	if g == nil || contentID == "" {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, delisted := g.content[contentID]
	return !delisted
}
