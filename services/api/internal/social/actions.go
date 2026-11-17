package social

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/records"
)

type result struct {
	status     int
	raw        []byte
	err        error
	generation int64
	cache      string
}

func done(status int, body any) result {
	raw, err := json.Marshal(body)
	if err != nil {
		return result{err: err}
	}
	return result{status: status, raw: raw}
}

func (s *Service) Follow(account, target, key, sum string, signed bool) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw, status, err := s.takeIdem(account, key, sum); err != nil {
		if errors.Is(err, errDuplicate) {
			return result{status: status, raw: raw}
		}
		return result{err: err}
	}
	if !idRE.MatchString(target) || target == account {
		return result{err: errMalformed}
	}
	edges := s.follows[account]
	if edges == nil {
		edges = map[string]edge{}
		s.follows[account] = edges
	}
	if len(edges) >= maxFollows && !hasEdge(edges, target) {
		return result{err: errMalformed}
	}
	prev, exists := edges[target]
	if !exists {
		if err := s.charge(account); err != nil {
			return result{err: err}
		}
	}
	duplicate := exists
	if !exists {
		edges[target] = edge{Signed: signed}
		s.addNote(target, "New follower "+account, "")
		s.bump()
	} else if signed && !prev.Signed {
		edges[target] = edge{Signed: true}
		s.bump()
	}
	out := done(statusOf(duplicate), map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        target,
		"signed":            edges[target].Signed,
		"public_record":     edges[target].Signed,
		"duplicate":         duplicate,
		"chain_event":       false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func hasEdge(edges map[string]edge, target string) bool {
	_, ok := edges[target]
	return ok
}

func statusOf(duplicate bool) int {
	if duplicate {
		return 200
	}
	return 201
}

func (s *Service) Unfollow(account, target, key, sum string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw, status, err := s.takeIdem(account, key, sum); err != nil {
		if errors.Is(err, errDuplicate) {
			return result{status: status, raw: raw}
		}
		return result{err: err}
	}
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	if !idRE.MatchString(target) {
		return result{err: errMalformed}
	}
	_, existed := s.follows[account][target]
	delete(s.follows[account], target)
	if existed {
		s.bump()
	}
	out := done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        target,
		"following":         false,
		"chain_event":       false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func (s *Service) Like(account, contentID, key, sum string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw, status, err := s.takeIdem(account, key, sum); err != nil {
		if errors.Is(err, errDuplicate) {
			return result{status: status, raw: raw}
		}
		return result{err: err}
	}
	it, ok := s.catalog[contentID]
	if !ok || !s.permitted(it) {
		return result{err: errNotFound}
	}
	likes := s.likes[account]
	if likes == nil {
		likes = map[string]string{}
		s.likes[account] = likes
	}
	id, duplicate := likes[contentID]
	if !duplicate {
		if err := s.charge(account); err != nil {
			return result{err: err}
		}
		id = s.newID("l")
		likes[contentID] = id
	}
	out := done(statusOf(duplicate), map[string]any{
		"interface_version": InterfaceVersion,
		"content_id":        contentID,
		"like_id":           id,
		"duplicate":         duplicate,
		"chain_event":       false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func (s *Service) Save(account, contentID, key, sum string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw, status, err := s.takeIdem(account, key, sum); err != nil {
		if errors.Is(err, errDuplicate) {
			return result{status: status, raw: raw}
		}
		return result{err: err}
	}
	it, ok := s.catalog[contentID]
	if !ok || !s.permitted(it) {
		return result{err: errNotFound}
	}
	saved := s.saves[account]
	if saved == nil {
		saved = map[string]struct{}{}
		s.saves[account] = saved
	}
	_, duplicate := saved[contentID]
	if !duplicate {
		if err := s.charge(account); err != nil {
			return result{err: err}
		}
	}
	saved[contentID] = struct{}{}
	out := done(statusOf(duplicate), map[string]any{
		"interface_version": InterfaceVersion,
		"content_id":        contentID,
		"duplicate":         duplicate,
		"private":           true,
		"chain_event":       false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func (s *Service) NotInterested(account, contentID, key, sum string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw, status, err := s.takeIdem(account, key, sum); err != nil {
		if errors.Is(err, errDuplicate) {
			return result{status: status, raw: raw}
		}
		return result{err: err}
	}
	if _, ok := s.catalog[contentID]; !ok {
		return result{err: errNotFound}
	}
	set := s.nope[account]
	if set == nil {
		set = map[string]struct{}{}
		s.nope[account] = set
	}
	_, duplicate := set[contentID]
	if !duplicate {
		if err := s.charge(account); err != nil {
			return result{err: err}
		}
	}
	set[contentID] = struct{}{}
	if !duplicate {
		s.bump()
	}
	out := done(statusOf(duplicate), map[string]any{
		"interface_version": InterfaceVersion,
		"content_id":        contentID,
		"duplicate":         duplicate,
		"private":           true,
		"chain_event":       false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func (s *Service) Comment(account, contentID, text, key, sum string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		return result{err: errMalformed}
	}
	if raw, status, err := s.takeIdem(account, key, sum); err != nil {
		if errors.Is(err, errDuplicate) {
			return result{status: status, raw: raw}
		}
		return result{err: err}
	}
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	it, ok := s.catalog[contentID]
	if !ok || !s.permitted(it) {
		return result{err: errNotFound}
	}
	clean, err := cleanText(text, maxComment)
	if err != nil {
		return result{err: err}
	}
	if s.commentRefused(clean) {
		return result{err: errRefused}
	}
	n := 0
	for _, row := range s.comments {
		if row.Content == contentID {
			n++
		}
	}
	if n >= maxComments {
		return result{err: errMalformed}
	}
	row := comment{ID: s.newID("m"), Author: account, Content: contentID, Text: clean, Seq: s.seq}
	s.comments = append(s.comments, row)
	if it.CreatorID != account {
		s.addNote(it.CreatorID, clean, row.ID)
	}
	out := done(201, map[string]any{
		"interface_version": InterfaceVersion,
		"comment_id":        row.ID,
		"content_id":        contentID,
		"text":              row.Text,
		"chain_event":       false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func (s *Service) commentRefused(text string) bool {
	folded := strings.ToLower(text)
	for phrase := range s.refuse {
		if phrase != "" && strings.Contains(folded, phrase) {
			return true
		}
	}
	return false
}

func cleanText(text string, limit int) (string, error) {
	if strings.ContainsAny(text, "<>") || strings.ContainsRune(text, '\u0000') {
		return "", errRefused
	}
	for _, r := range text {
		if r < 0x20 && r != '\n' && r != '\t' {
			return "", errRefused
		}
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len([]rune(trimmed)) > limit {
		return "", errMalformed
	}
	return trimmed, nil
}

func (s *Service) DeleteComment(account, commentID string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	idx := -1
	for i, row := range s.comments {
		if row.ID == commentID {
			idx = i
			if row.Author != account {
				return result{err: errForbidden}
			}
			break
		}
	}
	if idx < 0 {
		return result{err: errNotFound}
	}
	s.comments = append(s.comments[:idx], s.comments[idx+1:]...)
	for acct, list := range s.notes {
		kept := list[:0]
		for _, note := range list {
			if note.CommentID != commentID {
				kept = append(kept, note)
			}
		}
		s.notes[acct] = kept
	}
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"comment_id":        commentID,
		"deleted":           true,
		"chain_event":       false,
	})
}

func (s *Service) PutProfile(account, name string, topics []string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	clean, err := cleanText(name, maxTitle)
	if err != nil {
		return result{err: err}
	}
	parsed, err := cleanTopics(topics)
	if err != nil {
		return result{err: err}
	}
	s.profiles[account] = profile{Name: clean, Topics: parsed}
	s.bump()
	return done(200, publicProfile(account, s.profiles[account]))
}

func (s *Service) PutPrefs(account string, topics, hidden []string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	parsed, err := cleanTopics(topics)
	if err != nil {
		return result{err: err}
	}
	next := map[string]struct{}{}
	for _, id := range hidden {
		if !idRE.MatchString(id) {
			return result{err: errMalformed}
		}
		next[id] = struct{}{}
	}
	if len(next) > maxTopics {
		return result{err: errMalformed}
	}
	s.prefs[account] = pref{Topics: parsed}
	s.hidden[account] = next
	s.bump()
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"private":           true,
		"topics":            parsed,
	})
}

func cleanTopics(topics []string) ([]string, error) {
	if len(topics) > maxTopics {
		return nil, errMalformed
	}
	out := make([]string, 0, len(topics))
	seen := map[string]struct{}{}
	for _, topic := range topics {
		if !idRE.MatchString(topic) {
			return nil, errMalformed
		}
		if _, ok := seen[topic]; ok {
			continue
		}
		seen[topic] = struct{}{}
		out = append(out, topic)
	}
	return out, nil
}

func (s *Service) Upsert(account string, it item) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !idRE.MatchString(it.ContentID) {
		return result{err: errMalformed}
	}
	title, err := cleanText(it.Title, maxTitle)
	if err != nil {
		return result{err: err}
	}
	topics, err := cleanTopics(it.Topics)
	if err != nil {
		return result{err: err}
	}
	if it.PublishedAt < 0 {
		return result{err: errMalformed}
	}
	play, err := cleanPlayback(it.Playback)
	if err != nil {
		return result{err: err}
	}
	if prev, ok := s.catalog[it.ContentID]; ok {
		if prev.CreatorID != account {
			return result{err: errForbidden}
		}
		if prev.Withdrawn {
			return result{err: errConflict}
		}
		prev.Title = title
		prev.Topics = topics
		prev.Playback = play
		prev.Available = it.Available
		s.catalog[it.ContentID] = prev
		s.reindex()
		s.bump()
		return done(200, map[string]any{
			"interface_version": InterfaceVersion,
			"content_id":        prev.ContentID,
			"duplicate":         true,
			"chain_event":       false,
		})
	}
	n := 0
	for _, row := range s.catalog {
		if row.CreatorID == account {
			n++
		}
	}
	if n >= maxCatalog {
		return result{err: errMalformed}
	}
	it.CreatorID = account
	it.Title = title
	it.Topics = topics
	it.Playback = play
	it.Withdrawn = false
	s.catalog[it.ContentID] = it
	s.reindex()
	s.bump()
	return done(201, map[string]any{
		"interface_version": InterfaceVersion,
		"content_id":        it.ContentID,
		"duplicate":         false,
		"chain_event":       false,
	})
}

func cleanPlayback(urls []string) ([]string, error) {
	if len(urls) > maxPlayback {
		return nil, errMalformed
	}
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, errMalformed
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return nil, errMalformed
		}
		host := parsed.Hostname()
		if host != "127.0.0.1" && host != "localhost" {
			return nil, errMalformed
		}
		out = append(out, raw)
	}
	return out, nil
}

func (s *Service) Withdraw(account, contentID string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	it, ok := s.catalog[contentID]
	if !ok {
		return result{err: errNotFound}
	}
	if it.CreatorID != account {
		return result{err: errForbidden}
	}
	it.Withdrawn = true
	s.catalog[contentID] = it
	s.reindex()
	s.bump()
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"content_id":        contentID,
		"withdrawn":         true,
		"chain_event":       false,
	})
}

func (s *Service) RegisterKey(account, keyID, publicHex string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !idRE.MatchString(keyID) {
		return result{err: errMalformed}
	}
	raw, err := hex.DecodeString(publicHex)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return result{err: errMalformed}
	}
	if owner, ok := s.keyOwner[keyID]; ok && owner != account {
		return result{err: errConflict}
	}
	s.keys[keyID] = records.Key{Public: ed25519.PublicKey(raw), Active: true}
	s.keyOwner[keyID] = account
	return done(201, map[string]any{
		"interface_version": InterfaceVersion,
		"key_id":            keyID,
		"chain_event":       false,
	})
}

func (s *Service) AcceptPublic(account, canonical, sigHex string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return result{err: errMalformed}
	}
	var peeked struct {
		Payload struct {
			RecordType string `json:"record_type"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(canonical), &peeked) != nil || (peeked.Payload.RecordType != "follow" && peeked.Payload.RecordType != "unfollow") {
		return result{err: errMalformed}
	}
	env, err := records.Accept(&s.history, canonical, sig, s.keys)
	if err != nil {
		return result{err: errMalformed}
	}
	if env.AuthorID != account || s.keyOwner[env.KeyID] != account {
		return result{err: errForbidden}
	}
	target := env.Payload.Body
	if !idRE.MatchString(target) || target == account {
		return result{err: errMalformed}
	}
	sum := sha256.Sum256([]byte(canonical))
	s.public = append(s.public, publicRow{
		Author: account, RecordID: env.RecordID, Sequence: env.Sequence,
		Type: env.Payload.RecordType, Body: target, Canonical: canonical,
		Digest: hex.EncodeToString(sum[:]),
	})
	if env.Payload.RecordType == "follow" {
		return s.followLocked(account, target, true)
	}
	delete(s.follows[account], target)
	s.bump()
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        target,
		"signed":            true,
		"public_record":     true,
		"following":         false,
		"chain_event":       false,
	})
}

func (s *Service) followLocked(account, target string, signed bool) result {
	edges := s.follows[account]
	if edges == nil {
		edges = map[string]edge{}
		s.follows[account] = edges
	}
	_, exists := edges[target]
	if !exists {
		edges[target] = edge{Signed: signed}
		s.addNote(target, "New follower "+account, "")
	} else if signed {
		edges[target] = edge{Signed: true}
	}
	s.bump()
	return done(201, map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        target,
		"signed":            edges[target].Signed,
		"public_record":     true,
		"duplicate":         exists,
		"chain_event":       false,
	})
}

func publicProfile(account string, row profile) map[string]any {
	topics := row.Topics
	if topics == nil {
		topics = []string{}
	}
	return map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        account,
		"display_name":      row.Name,
		"topics":            topics,
	}
}

func (s *Service) Profile(account string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.profiles[account]
	if !ok {
		return result{err: errNotFound}
	}
	return done(200, publicProfile(account, row))
}

func (s *Service) Saves(account string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.saves[account]))
	for id := range s.saves[account] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"private":           true,
		"content_ids":       ids,
		"chain_event":       false,
	})
}

func (s *Service) Likes(account string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.likes[account]))
	for id := range s.likes[account] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"private":           true,
		"content_ids":       ids,
	})
}

func (s *Service) PublicFollows(account string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0)
	for target, edge := range s.follows[account] {
		if edge.Signed {
			ids = append(ids, target)
		}
	}
	sort.Strings(ids)
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        account,
		"follows":           ids,
	})
}

func (s *Service) PublicRecords(account string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]map[string]any, 0)
	for _, row := range s.public {
		if row.Author != account {
			continue
		}
		rows = append(rows, map[string]any{
			"record_id": row.RecordID,
			"sequence":  row.Sequence,
			"type":      row.Type,
			"digest":    row.Digest,
		})
	}
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        account,
		"history_erased":    false,
		"decision_id":       "DEC-004",
		"value_version":     0,
		"records":           rows,
	})
}

func (s *Service) DeleteAccount(account string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := 0
	for _, row := range s.public {
		if row.Author == account {
			kept++
		}
	}
	delete(s.profiles, account)
	delete(s.prefs, account)
	delete(s.follows, account)
	for acct, edges := range s.follows {
		delete(edges, account)
		s.follows[acct] = edges
	}
	delete(s.likes, account)
	delete(s.saves, account)
	delete(s.nope, account)
	delete(s.hidden, account)
	delete(s.notes, account)
	for id, it := range s.catalog {
		if it.CreatorID == account {
			it.Withdrawn = true
			s.catalog[id] = it
		}
	}
	s.reindex()
	keptComments := s.comments[:0]
	for _, row := range s.comments {
		if row.Author != account {
			keptComments = append(keptComments, row)
		}
	}
	s.comments = keptComments
	for token, id := range s.tokens {
		if id == account {
			delete(s.tokens, token)
		}
	}
	s.bump()
	return done(200, map[string]any{
		"interface_version":   InterfaceVersion,
		"history_erased":      false,
		"decision_id":         "DEC-004",
		"value_version":       0,
		"public_records_kept": kept,
		"chain_event":         false,
	})
}

func (s *Service) Comments(contentID, cursor string, limit int) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.catalog[contentID]
	if !ok || !s.permitted(it) {
		return result{err: errNotFound}
	}
	after, err := parseSeq(cursor)
	if err != nil {
		return result{err: err}
	}
	limit = clampLimit(limit)
	rows := make([]map[string]any, 0)
	var last int64
	for _, row := range s.comments {
		if row.Content != contentID || row.Seq <= after {
			continue
		}
		if len(rows) == limit {
			return done(200, map[string]any{
				"interface_version": InterfaceVersion,
				"content_id":        contentID,
				"comments":          rows,
				"next_cursor":       strconv.FormatInt(last, 10),
			})
		}
		rows = append(rows, map[string]any{
			"comment_id": row.ID,
			"author_id":  row.Author,
			"text":       row.Text,
		})
		last = row.Seq
	}
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"content_id":        contentID,
		"comments":          rows,
		"next_cursor":       "",
	})
}

func parseSeq(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || n < 0 {
		return 0, errMalformed
	}
	return n, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return pageDefault
	}
	if limit > pageMax {
		return pageMax
	}
	return limit
}

func (s *Service) Activity(account, cursor string, limit int) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := parseSeq(cursor)
	if err != nil {
		return result{err: err}
	}
	if cursor == "" {
		before = 1 << 62
	}
	limit = clampLimit(limit)
	list := append([]note(nil), s.notes[account]...)
	sort.Slice(list, func(i, j int) bool { return list[i].Seq > list[j].Seq })
	rows := make([]map[string]any, 0)
	var last int64
	for _, row := range list {
		if row.Seq >= before {
			continue
		}
		if len(rows) == limit {
			return done(200, map[string]any{
				"interface_version": InterfaceVersion,
				"private":           true,
				"notes":             rows,
				"next_cursor":       strconv.FormatInt(last, 10),
			})
		}
		rows = append(rows, map[string]any{"note_id": row.ID, "text": row.Text})
		last = row.Seq
	}
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"private":           true,
		"notes":             rows,
		"next_cursor":       "",
	})
}
