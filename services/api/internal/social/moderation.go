package social

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/moderation"
)

var reasonRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

type modCase struct {
	ID         string
	Reporter   string
	TargetType string
	TargetID   string
	Subject    string
	Category   string
	Narrative  string
	Status     string
	Outcome    string
	ReasonCode string
	Appeal     string
	AppealNote string
	Escalation string
	Spam       int
}

type auditRow struct {
	ID         string
	Actor      string
	Action     string
	Target     string
	ReasonCode string
	At         string
}

type reportBody struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Category   string `json:"category"`
	Narrative  string `json:"narrative"`
}

func (s *Service) setRemoval(opt moderation.Options) {
	if opt.Now == nil {
		opt.Now = s.now
	}
	s.mu.Lock()
	s.coord = moderation.New(opt)
	s.mu.Unlock()
}

func (s *Service) isMod(account string) bool {
	_, ok := s.mods[account]
	return ok
}

func (s *Service) blocked(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if _, ok := s.blocks[a][b]; ok {
		return true
	}
	_, ok := s.blocks[b][a]
	return ok
}

func (s *Service) addAudit(actor, action, target, reason string) {
	s.audits = append(s.audits, auditRow{
		ID: s.newID("a"), Actor: actor, Action: action, Target: target,
		ReasonCode: reason, At: s.now().UTC().Format(time.RFC3339),
	})
}

func (s *Service) report(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body reportBody
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.fileReport(account, key, sum, body))
}

func (s *Service) fileReport(account, key, sum string, body reportBody) result {
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
	narrative := strings.TrimSpace(body.Narrative)
	if (body.TargetType != "content" && body.TargetType != "account" && body.TargetType != "comment") ||
		!idRE.MatchString(body.TargetID) || (body.Category != "spam" && body.Category != "other") ||
		narrative == "" || len([]rune(narrative)) > 400 {
		return result{err: errMalformed}
	}
	index := account + "\x00" + body.TargetType + "\x00" + body.TargetID
	if existing, ok := s.caseBy[index]; ok {
		s.cases[existing].Spam++
		out := done(200, map[string]any{
			"interface_version": InterfaceVersion,
			"case_id":           existing,
			"status":            "spam",
			"duplicate":         true,
			"narrative_stored":  false,
			"chain_event":       false,
		})
		s.saveIdem(account, key, sum, out.status, out.raw)
		return out
	}
	row := &modCase{
		ID: s.newID("k"), Reporter: account, TargetType: body.TargetType, TargetID: body.TargetID,
		Subject: s.subject(body.TargetType, body.TargetID), Category: body.Category, Narrative: narrative,
		Status: "open", Appeal: "none", Escalation: "none",
	}
	s.cases[row.ID] = row
	s.caseBy[index] = row.ID
	out := done(201, map[string]any{
		"interface_version": InterfaceVersion,
		"case_id":           row.ID,
		"status":            "open",
		"narrative_stored":  true,
		"chain_event":       false,
		"legal_approval":    false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func (s *Service) subject(kind, id string) string {
	switch kind {
	case "account":
		return id
	case "content":
		if it, ok := s.catalog[id]; ok {
			return it.CreatorID
		}
	case "comment":
		for _, row := range s.comments {
			if row.ID == id {
				return row.Author
			}
		}
	}
	return ""
}

func (s *Service) listReports(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]map[string]any, 0)
	for _, row := range s.sortedCases() {
		if row.Reporter != account {
			continue
		}
		rows = append(rows, map[string]any{
			"case_id": row.ID, "status": row.Status, "target_id": row.TargetID, "target_type": row.TargetType,
		})
	}
	writeResult(w, done(200, map[string]any{
		"interface_version": InterfaceVersion, "cases": rows, "chain_event": false,
	}))
}

func (s *Service) block(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		AccountID string `json:"account_id"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.blockAccount(account, body.AccountID, key, sum))
}

func (s *Service) blockAccount(account, target, key, sum string) result {
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
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	if s.blocks[account] == nil {
		s.blocks[account] = map[string]struct{}{}
	}
	_, exists := s.blocks[account][target]
	s.blocks[account][target] = struct{}{}
	if !exists {
		s.bump()
	}
	out := done(statusOf(exists), map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        target,
		"blocked":           true,
		"duplicate":         exists,
		"chain_event":       false,
	})
	s.saveIdem(account, key, sum, out.status, out.raw)
	return out
}

func (s *Service) listBlocks(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0)
	for id := range s.blocks[account] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	writeResult(w, done(200, map[string]any{
		"interface_version": InterfaceVersion, "blocked": ids, "chain_event": false,
	}))
}

func (s *Service) queue(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isMod(account) {
		writeAPI(w, errForbidden)
		return
	}
	rows := make([]map[string]any, 0)
	for _, row := range s.sortedCases() {
		if row.Status == "open" || row.Appeal == "open" {
			rows = append(rows, s.caseView(account, row))
		}
	}
	writeResult(w, done(200, map[string]any{
		"interface_version": InterfaceVersion, "queue": rows, "chain_event": false,
	}))
}

func (s *Service) audit(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isMod(account) {
		writeAPI(w, errForbidden)
		return
	}
	rows := make([]map[string]any, 0, len(s.audits))
	for _, row := range s.audits {
		rows = append(rows, map[string]any{
			"audit_id": row.ID, "actor": row.Actor, "action": row.Action,
			"target": row.Target, "reason_code": row.ReasonCode, "at": row.At,
		})
	}
	writeResult(w, done(200, map[string]any{
		"interface_version": InterfaceVersion, "audit": rows, "chain_event": false,
	}))
}

func (s *Service) getCase(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.cases[r.PathValue("id")]
	if row == nil {
		writeAPI(w, errNotFound)
		return
	}
	if !s.isMod(account) && account != row.Reporter && account != row.Subject {
		writeAPI(w, errForbidden)
		return
	}
	writeResult(w, done(200, s.caseView(account, row)))
}

func (s *Service) caseView(viewer string, row *modCase) map[string]any {
	out := map[string]any{
		"interface_version":  InterfaceVersion,
		"case_id":            row.ID,
		"status":             row.Status,
		"target_type":        row.TargetType,
		"target_id":          row.TargetID,
		"outcome":            row.Outcome,
		"reason_code":        row.ReasonCode,
		"appeal_status":      row.Appeal,
		"escalation_status":  row.Escalation,
		"appeal_authority":   "PLACEHOLDER. No appeal authority is named.",
		"spam_reports":       row.Spam,
		"chain_event":        false,
		"narrative_retained": row.Narrative != "",
		"legal_approval":     false,
		"public_launch":      "DISABLED",
	}
	if s.isMod(viewer) {
		out["complainant"] = row.Reporter
		if row.Narrative != "" {
			out["narrative"] = row.Narrative
		}
		if row.AppealNote != "" {
			out["appeal_note"] = row.AppealNote
		}
	}
	return out
}

func (s *Service) decide(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		Outcome    string `json:"outcome"`
		ReasonCode string `json:"reason_code"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.decideCase(account, r.PathValue("id"), body.Outcome, body.ReasonCode))
}

func (s *Service) decideCase(actor, caseID, outcome, reason string) result {
	s.mu.Lock()
	if !s.isMod(actor) {
		s.mu.Unlock()
		return result{err: errForbidden}
	}
	row := s.cases[caseID]
	if row == nil {
		s.mu.Unlock()
		return result{err: errNotFound}
	}
	if !reasonRE.MatchString(reason) {
		s.mu.Unlock()
		return result{err: errMalformed}
	}
	if err := s.charge(actor); err != nil {
		s.mu.Unlock()
		return result{err: err}
	}
	if row.Appeal == "open" {
		if outcome != "upheld" && outcome != "reversed" {
			s.mu.Unlock()
			return result{err: errMalformed}
		}
		row.Appeal = outcome
		row.AppealNote = ""
		if outcome == "reversed" {
			s.reverse(row)
		}
		s.addAudit(actor, "appeal_decision", caseID, reason)
		view := s.caseView(actor, row)
		s.mu.Unlock()
		return done(200, view)
	}
	if row.Status == "decided" {
		s.mu.Unlock()
		return result{err: errConflict}
	}
	if outcome != "remove" && outcome != "dismiss" && outcome != "takedown" {
		s.mu.Unlock()
		return result{err: errMalformed}
	}
	row.Narrative = ""
	row.Status = "decided"
	row.Outcome = outcome
	row.ReasonCode = reason
	ids := s.applyOutcome(row, outcome)
	s.addAudit(actor, "decision", caseID, reason)
	view := s.caseView(actor, row)
	s.mu.Unlock()
	reports := make([]moderation.Report, 0, len(ids))
	ok := true
	for _, id := range ids {
		report := s.coord.Remove(id)
		reports = append(reports, report)
		if !report.SuccessfulDeletion {
			ok = false
		}
	}
	view["removals"] = reports
	view["narrative_retained"] = false
	view["universal_deletion"] = false
	view["ledger_retained"] = true
	view["successful_deletion"] = ok && outcome != "dismiss"
	if outcome == "dismiss" {
		view["successful_deletion"] = false
	}
	return done(200, view)
}

func (s *Service) applyOutcome(row *modCase, outcome string) []string {
	ids := make([]string, 0)
	switch outcome {
	case "remove":
		if row.TargetType == "content" {
			s.hideItem[row.TargetID] = struct{}{}
			ids = append(ids, row.TargetID)
		}
		if row.TargetType == "comment" {
			s.dropComment(row.TargetID)
		}
	case "takedown":
		s.suspend(row.TargetID)
		for id, it := range s.catalog {
			if it.CreatorID == row.TargetID {
				s.hideItem[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	s.reindex()
	s.bump()
	return ids
}

func (s *Service) reverse(row *modCase) {
	if row.Outcome == "remove" && row.TargetType == "content" {
		if _, sticky := s.sticky[row.TargetID]; !sticky {
			delete(s.hideItem, row.TargetID)
		}
	}
	if row.Outcome == "takedown" {
		delete(s.suspended, row.TargetID)
		delete(s.hideAcct, row.TargetID)
	}
	s.reindex()
	s.bump()
}

func (s *Service) suspend(account string) {
	s.suspended[account] = struct{}{}
	s.hideAcct[account] = struct{}{}
	for token, id := range s.tokens {
		if id == account {
			delete(s.tokens, token)
		}
	}
}

func (s *Service) dropComment(commentID string) {
	kept := s.comments[:0]
	for _, row := range s.comments {
		if row.ID != commentID {
			kept = append(kept, row)
		}
	}
	s.comments = kept
}

func (s *Service) appeal(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		Statement string `json:"statement"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.appealCase(account, r.PathValue("id"), body.Statement))
}

func (s *Service) appealCase(account, caseID, statement string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.cases[caseID]
	if row == nil {
		return result{err: errNotFound}
	}
	if row.Status != "decided" || row.Appeal != "none" {
		return result{err: errConflict}
	}
	if account != row.Subject {
		return result{err: errForbidden}
	}
	text := strings.TrimSpace(statement)
	if text == "" || len([]rune(text)) > 400 {
		return result{err: errMalformed}
	}
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	row.Appeal = "open"
	row.AppealNote = text
	s.addAudit(account, "appeal", caseID, "appeal_open")
	view := s.caseView(account, row)
	delete(view, "appeal_note")
	delete(view, "narrative")
	delete(view, "complainant")
	return done(201, view)
}

func (s *Service) escalate(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	_ = body.Note
	writeResult(w, s.escalateCase(account, r.PathValue("id")))
}

func (s *Service) escalateCase(account, caseID string) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.cases[caseID]
	if row == nil {
		return result{err: errNotFound}
	}
	if err := s.charge(account); err != nil {
		return result{err: err}
	}
	if s.isMod(account) {
		row.Escalation = "escalated"
		s.addAudit(account, "escalation", caseID, "escalated")
		return done(200, s.caseView(account, row))
	}
	if account != row.Subject && account != row.Reporter {
		return result{err: errForbidden}
	}
	if row.Escalation == "escalated" {
		return result{err: errConflict}
	}
	row.Escalation = "requested"
	return done(200, s.caseView(account, row))
}

func (s *Service) takedown(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.takeDown(account, r.PathValue("id")))
}

func (s *Service) takeDown(actor, target string) result {
	s.mu.Lock()
	if !s.isMod(actor) {
		s.mu.Unlock()
		return result{err: errForbidden}
	}
	if !idRE.MatchString(target) {
		s.mu.Unlock()
		return result{err: errMalformed}
	}
	if err := s.charge(actor); err != nil {
		s.mu.Unlock()
		return result{err: err}
	}
	s.suspend(target)
	ids := make([]string, 0)
	for id, it := range s.catalog {
		if it.CreatorID == target {
			s.hideItem[id] = struct{}{}
			s.sticky[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	s.reindex()
	s.bump()
	s.addAudit(actor, "account_takedown", target, "account_takedown")
	gen := s.gen
	s.mu.Unlock()
	sort.Strings(ids)
	reports := make([]moderation.Report, 0, len(ids))
	ok := len(ids) > 0
	for _, id := range ids {
		report := s.coord.Remove(id)
		reports = append(reports, report)
		if !report.SuccessfulDeletion {
			ok = false
		}
	}
	return done(200, map[string]any{
		"interface_version":   InterfaceVersion,
		"account_id":          target,
		"suspended":           true,
		"history_erased":      false,
		"ledger_retained":     true,
		"universal_deletion":  false,
		"successful_deletion": ok,
		"cache_generation":    gen,
		"removals":            reports,
		"chain_event":         false,
		"legal_approval":      false,
		"retention":           "DEC-004 unset; minimum records kept",
	})
}

func (s *Service) removal(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		ContentID string `json:"content_id"`
		Confirm   string `json:"confirm"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.requestRemoval(account, body.ContentID, body.Confirm, key, sum))
}

func (s *Service) requestRemoval(account, contentID, confirm, key, sum string) result {
	s.mu.Lock()
	if raw, status, err := s.takeIdem(account, key, sum); err != nil {
		s.mu.Unlock()
		if errors.Is(err, errDuplicate) {
			return result{status: status, raw: raw}
		}
		return result{err: err}
	}
	if confirm != "withdraw" || !idRE.MatchString(contentID) {
		s.mu.Unlock()
		return result{err: errMalformed}
	}
	if err := s.charge(account); err != nil {
		s.mu.Unlock()
		return result{err: err}
	}
	if !s.mayRemove(account, contentID) {
		s.mu.Unlock()
		return result{err: errForbidden}
	}
	s.hideItem[contentID] = struct{}{}
	s.sticky[contentID] = struct{}{}
	s.reindex()
	s.bump()
	gen := s.gen
	s.addAudit(account, "creator_removal", contentID, "creator_request")
	s.mu.Unlock()
	report := s.coord.Remove(contentID)
	body := map[string]any{}
	encoded, err := json.Marshal(report)
	if err != nil {
		return result{err: err}
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		return result{err: err}
	}
	body["interface_version"] = InterfaceVersion
	body["cache_generation"] = gen
	body["chain_event"] = false
	out := done(http.StatusAccepted, body)
	s.mu.Lock()
	s.saveIdem(account, key, sum, out.status, out.raw)
	s.mu.Unlock()
	return out
}

func (s *Service) mayRemove(account, contentID string) bool {
	if s.isMod(account) {
		return true
	}
	it, ok := s.catalog[contentID]
	return ok && it.CreatorID == account
}

func (s *Service) policyPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.RawQuery != "" {
		writeAPI(w, errMalformed)
		return
	}
	s.mu.Lock()
	doc := s.policy
	s.mu.Unlock()
	writeResult(w, done(200, doc))
}

func (s *Service) devModerator(w http.ResponseWriter, r *http.Request) {
	if !s.dev {
		writeAPI(w, errNotFound)
		return
	}
	if r.URL.RawQuery != "" {
		writeAPI(w, errMalformed)
		return
	}
	var body struct {
		AccountID string `json:"account_id"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil || decodeRaw(raw, &body) != nil || !idRE.MatchString(body.AccountID) {
		writeAPI(w, errMalformed)
		return
	}
	s.mu.Lock()
	s.mods[body.AccountID] = struct{}{}
	s.mu.Unlock()
	writeResult(w, done(200, map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        body.AccountID,
		"role":              "moderator",
		"chain_event":       false,
	}))
}

func (s *Service) sortedCases() []*modCase {
	rows := make([]*modCase, 0, len(s.cases))
	for _, row := range s.cases {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}
