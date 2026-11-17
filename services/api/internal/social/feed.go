package social

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

type scored struct {
	item  item
	score int
}

func (s *Service) Feed(account, kind, cursor string, limit int, cachedGen int64) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != "following" && kind != "recommended" {
		return result{err: errNotFound}
	}
	limit = clampLimit(limit)
	if cursor != "" {
		if _, _, err := splitCursor(cursor); err != nil {
			return result{err: err}
		}
	}
	key := account + "\x00" + kind + "\x00" + cursor + "\x00" + strconv.Itoa(limit)
	if cachedGen == s.gen {
		if page, ok := s.cache[key]; ok {
			return result{status: 200, raw: page.raw, generation: s.gen, cache: "hit"}
		}
	}
	flag := "miss"
	if cachedGen >= 0 && cachedGen != s.gen {
		flag = "refreshed"
	}
	var rows []item
	var factors []string
	if kind == "following" {
		rows = s.following(account)
		factors = []string{}
	} else {
		rows = s.recommended(account)
		factors = rankingFactors()
	}
	page, next := slicePage(rows, cursor, limit)
	body := feedDoc{
		InterfaceVersion: InterfaceVersion,
		Kind:             kind,
		CacheGeneration:  s.gen,
		ChainEvents:      false,
		ScoringFactors:   factors,
		Items:            cards(page),
		NextCursor:       next,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return result{err: err}
	}
	s.cache[key] = cached{raw: raw}
	return result{status: 200, raw: raw, generation: s.gen, cache: flag}
}

func (s *Service) following(account string) []item {
	var rows []item
	for _, it := range s.catalog {
		if !s.visible(account, it) {
			continue
		}
		if _, ok := s.follows[account][it.CreatorID]; !ok {
			continue
		}
		rows = append(rows, it)
	}
	sortItems(rows)
	return rows
}

func (s *Service) recommended(account string) []item {
	newest := int64(0)
	for _, it := range s.catalog {
		if s.permitted(it) && it.PublishedAt > newest {
			newest = it.PublishedAt
		}
	}
	want := map[string]struct{}{}
	for _, topic := range s.prefs[account].Topics {
		want[topic] = struct{}{}
	}
	var rows []scored
	for _, it := range s.catalog {
		if !s.visible(account, it) {
			continue
		}
		_, followed := s.follows[account][it.CreatorID]
		topic := false
		for _, name := range it.Topics {
			if _, ok := want[name]; ok {
				topic = true
				break
			}
		}
		recent := newest > 0 && it.PublishedAt >= newest-recentWindow
		if !followed && !topic && !recent {
			continue
		}
		score := 0
		if followed {
			score += scoreFollow
		}
		if topic {
			score += scoreTopic
		}
		if recent {
			score += scoreRecent
		}
		rows = append(rows, scored{item: it, score: score})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		if rows[i].item.PublishedAt != rows[j].item.PublishedAt {
			return rows[i].item.PublishedAt > rows[j].item.PublishedAt
		}
		return rows[i].item.ContentID < rows[j].item.ContentID
	})
	count := map[string]int{}
	out := make([]item, 0, len(rows))
	for _, row := range rows {
		if count[row.item.CreatorID] >= creatorCap {
			continue
		}
		count[row.item.CreatorID]++
		out = append(out, row.item)
	}
	return out
}

func (s *Service) visible(account string, it item) bool {
	if !s.permitted(it) {
		return false
	}
	if _, ok := s.hidden[account][it.CreatorID]; ok {
		return false
	}
	if s.blocked(account, it.CreatorID) {
		return false
	}
	if _, ok := s.nope[account][it.ContentID]; ok {
		return false
	}
	return true
}

func sortItems(rows []item) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].PublishedAt != rows[j].PublishedAt {
			return rows[i].PublishedAt > rows[j].PublishedAt
		}
		return rows[i].ContentID < rows[j].ContentID
	})
}

func slicePage(rows []item, cursor string, limit int) ([]item, string) {
	start := 0
	if cursor != "" {
		at, id, err := splitCursor(cursor)
		if err != nil {
			return nil, ""
		}
		for start < len(rows) && !after(rows[start], at, id) {
			start++
		}
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	page := rows[start:end]
	if end < len(rows) && len(page) > 0 {
		last := page[len(page)-1]
		return page, strconv.FormatInt(last.PublishedAt, 10) + "~" + last.ContentID
	}
	return page, ""
}

func after(it item, at int64, id string) bool {
	if it.PublishedAt != at {
		return it.PublishedAt < at
	}
	return it.ContentID > id
}

func splitCursor(cursor string) (int64, string, error) {
	at, id, ok := strings.Cut(cursor, "~")
	if !ok || !idRE.MatchString(id) {
		return 0, "", errMalformed
	}
	n, err := strconv.ParseInt(at, 10, 64)
	if err != nil || n < 0 {
		return 0, "", errMalformed
	}
	return n, id, nil
}

func cards(rows []item) []card {
	out := make([]card, 0, len(rows))
	for _, it := range rows {
		play := make([]loc, 0, len(it.Playback))
		for _, raw := range it.Playback {
			play = append(play, loc{URL: raw})
		}
		topics := it.Topics
		if topics == nil {
			topics = []string{}
		}
		out = append(out, card{
			ContentID: it.ContentID, CreatorID: it.CreatorID, Title: it.Title,
			Topics: topics, PublishedAt: it.PublishedAt, Playback: play,
		})
	}
	return out
}

func (s *Service) Search(account, kind, q string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if !queryRE.MatchString(q) {
		return result{err: errMalformed}
	}
	hits := make([]map[string]any, 0)
	switch kind {
	case "profile":
		ids := make([]string, 0, len(s.profiles))
		for id := range s.profiles {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			row := s.profiles[id]
			if strings.Contains(strings.ToLower(row.Name), q) {
				hits = append(hits, map[string]any{"kind": "profile", "account_id": id, "text": row.Name})
			}
		}
	case "video":
		ids := make([]string, 0, len(s.index))
		for id := range s.index {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			it := s.index[id]
			if strings.Contains(strings.ToLower(it.Title), q) {
				hits = append(hits, map[string]any{"kind": "video", "content_id": id, "text": it.Title})
			}
		}
	case "topic":
		ids := make([]string, 0, len(s.index))
		for id := range s.index {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			it := s.index[id]
			for _, topic := range it.Topics {
				if strings.Contains(topic, q) {
					hits = append(hits, map[string]any{"kind": "topic", "content_id": id, "text": topic})
					break
				}
			}
		}
	default:
		return result{err: errMalformed}
	}
	if len(hits) > pageMax {
		hits = hits[:pageMax]
	}
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"kind":              kind,
		"hits":              hits,
		"chain_events":      false,
	})
}

func (s *Service) Rebuild(account string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	s.index = map[string]item{}
	s.reindex()
	ids := make([]string, 0, len(s.index))
	permitted := 0
	for _, it := range s.catalog {
		if s.permitted(it) {
			permitted++
		}
	}
	for id := range s.index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	s.bump()
	return done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"permitted":         permitted,
		"indexed":           len(s.index),
		"equal":             permitted == len(s.index),
		"content_ids":       ids,
		"chain_events":      false,
	})
}

// CacheState reports whether a generation is the current one. Tests use it
// to show a stored page from an older generation is stale.
func (s *Service) Generation() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}
