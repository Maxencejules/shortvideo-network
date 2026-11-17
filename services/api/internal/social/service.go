package social

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/records"
)

const (
	InterfaceVersion = 1
	// writeBudget is the per-account write and search allowance inside one
	// minute. It is not part of the discovery document.
	writeBudget = 8
	pageDefault = 10
	pageMax     = 20
	maxComment  = 400
	maxTitle    = 80
	maxTopics   = 4
	maxPlayback = 4
	maxCatalog  = 100
	maxFollows  = 200
	maxComments = 100
	maxNotes    = 100
	scoreFollow = 3
	scoreTopic  = 2
	scoreRecent = 1
	// recentWindow is how far behind the newest permitted item still counts
	// as recent. It is a ranking input, not an abuse threshold.
	recentWindow int64 = 10_000
	creatorCap         = 2
)

var (
	errAuth      = errors.New("auth")
	errMalformed = errors.New("malformed")
	errForbidden = errors.New("forbidden")
	errConflict  = errors.New("conflict")
	errRate      = errors.New("rate")
	errRefused   = errors.New("refused")
	errNotFound  = errors.New("notfound")
	errDeferred  = errors.New("deferred")
	errDuplicate = errors.New("duplicate")

	idRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	idemRE  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,80}$`)
	queryRE = regexp.MustCompile(`^[a-z0-9][a-z0-9 ._-]{0,31}$`)
)

// Options configures the in-memory service. Now is injectable so tests do
// not depend on the wall clock. Dev registers the loopback session route.
type Options struct {
	Dev bool
	Now func() time.Time
}

type Service struct {
	mu   sync.Mutex
	dev  bool
	now  func() time.Time
	seq  int64
	gen  int64
	hits map[string][]time.Time
	idem map[string]idemRec

	tokens   map[string]string
	profiles map[string]profile
	prefs    map[string]pref
	follows  map[string]map[string]edge
	likes    map[string]map[string]string
	saves    map[string]map[string]struct{}
	nope     map[string]map[string]struct{}
	hidden   map[string]map[string]struct{}
	comments []comment
	notes    map[string][]note
	catalog  map[string]item
	index    map[string]item
	hideItem map[string]struct{}
	hideAcct map[string]struct{}
	refuse   map[string]struct{}
	keys     map[string]records.Key
	keyOwner map[string]string
	history  records.History
	public   []publicRow
	cache    map[string]cached
}

type idemRec struct {
	sum    string
	status int
	raw    []byte
}

type profile struct {
	Name   string
	Topics []string
}

type pref struct {
	Topics []string
}

type edge struct {
	Signed bool
}

type comment struct {
	ID      string
	Author  string
	Content string
	Text    string
	Seq     int64
}

type note struct {
	ID        string
	Text      string
	CommentID string
	Seq       int64
}

type item struct {
	ContentID   string
	CreatorID   string
	Title       string
	Topics      []string
	PublishedAt int64
	Available   bool
	Withdrawn   bool
	Playback    []string
}

type publicRow struct {
	Author    string
	RecordID  string
	Sequence  int64
	Type      string
	Body      string
	Canonical string
	Digest    string
}

type cached struct {
	raw []byte
}

type card struct {
	ContentID   string   `json:"content_id"`
	CreatorID   string   `json:"creator_id"`
	Title       string   `json:"title"`
	Topics      []string `json:"topics"`
	PublishedAt int64    `json:"published_at"`
	Playback    []loc    `json:"playback"`
}

type loc struct {
	URL string `json:"url"`
}

type feedDoc struct {
	InterfaceVersion int      `json:"interface_version"`
	Kind             string   `json:"kind"`
	CacheGeneration  int64    `json:"cache_generation"`
	ChainEvents      bool     `json:"chain_events"`
	ScoringFactors   []string `json:"scoring_factors"`
	Items            []card   `json:"items"`
	NextCursor       string   `json:"next_cursor"`
}

// New returns an empty in-memory discovery service.
func New(opt Options) *Service {
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		dev: opt.Dev, now: now,
		hits: map[string][]time.Time{}, idem: map[string]idemRec{},
		tokens: map[string]string{}, profiles: map[string]profile{},
		prefs: map[string]pref{}, follows: map[string]map[string]edge{},
		likes: map[string]map[string]string{}, saves: map[string]map[string]struct{}{},
		nope: map[string]map[string]struct{}{}, hidden: map[string]map[string]struct{}{},
		notes: map[string][]note{}, catalog: map[string]item{}, index: map[string]item{},
		hideItem: map[string]struct{}{}, hideAcct: map[string]struct{}{},
		refuse: map[string]struct{}{}, keys: map[string]records.Key{},
		keyOwner: map[string]string{}, cache: map[string]cached{},
	}
}

// Issue mints a bearer for an account id. The token is not a chain key.
func (s *Service) Issue(account string) (string, error) {
	if !idRE.MatchString(account) {
		return "", errMalformed
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	s.mu.Lock()
	s.tokens[token] = account
	s.mu.Unlock()
	return token, nil
}

// Hide drops a content id from discovery. Goal 23 will call this from a
// moderator decision. There is no HTTP route for it.
func (s *Service) Hide(contentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hideItem[contentID] = struct{}{}
	s.reindex()
	s.bump()
}

// HideCreator drops that creator's items from every feed.
func (s *Service) HideCreator(account string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hideAcct[account] = struct{}{}
	s.reindex()
	s.bump()
}

// DisallowComment refuses later comments that contain phrase. The phrase is
// not returned by any route.
func (s *Service) DisallowComment(phrase string) {
	s.mu.Lock()
	s.refuse[strings.ToLower(strings.TrimSpace(phrase))] = struct{}{}
	s.mu.Unlock()
}

func (s *Service) account(token string) (string, error) {
	if token == "" {
		return "", errAuth
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.tokens[token]
	if !ok {
		return "", errAuth
	}
	return id, nil
}

func (s *Service) charge(account string) error {
	now := s.now().UTC()
	cut := now.Add(-time.Minute)
	prev := s.hits[account]
	kept := prev[:0]
	for _, at := range prev {
		if at.After(cut) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= writeBudget {
		s.hits[account] = kept
		return errRate
	}
	s.hits[account] = append(kept, now)
	return nil
}

func (s *Service) takeIdem(account, key, sum string) ([]byte, int, error) {
	if key == "" {
		return nil, 0, nil
	}
	if !idemRE.MatchString(key) {
		return nil, 0, errMalformed
	}
	prev, ok := s.idem[account+"\x00"+key]
	if !ok {
		return nil, 0, nil
	}
	if prev.sum != sum {
		return nil, 0, errConflict
	}
	return append([]byte(nil), prev.raw...), prev.status, errDuplicate
}

func (s *Service) saveIdem(account, key, sum string, status int, raw []byte) {
	if key == "" {
		return
	}
	s.idem[account+"\x00"+key] = idemRec{sum: sum, status: status, raw: append([]byte(nil), raw...)}
}

func (s *Service) bump() {
	s.gen++
	s.cache = map[string]cached{}
}

func (s *Service) reindex() {
	s.index = map[string]item{}
	for id, it := range s.catalog {
		if s.permitted(it) {
			s.index[id] = it
		}
	}
}

func (s *Service) permitted(it item) bool {
	if !it.Available || it.Withdrawn {
		return false
	}
	if _, ok := s.hideItem[it.ContentID]; ok {
		return false
	}
	if _, ok := s.hideAcct[it.CreatorID]; ok {
		return false
	}
	return true
}

func (s *Service) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *Service) addNote(account, text, commentID string) {
	notes := s.notes[account]
	notes = append(notes, note{ID: s.newID("n"), Text: text, CommentID: commentID, Seq: s.nextSeq()})
	if len(notes) > maxNotes {
		notes = notes[len(notes)-maxNotes:]
	}
	s.notes[account] = notes
}

func (s *Service) newID(prefix string) string {
	return prefix + strconvHex(s.nextSeq())
}

func strconvHex(n int64) string {
	return strings.Repeat("0", 0) + hex.EncodeToString([]byte{
		byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n),
	})
}

func rankingFactors() []string {
	return []string{
		"followed creator +3",
		"shared topic +2",
		"published inside the recent window +1",
		"at most 2 items from one creator",
	}
}
