// Package moderation is the off-chain community policy for this process.
//
// Case files, complaint text, and audit rows stay in this process. They are
// not chain transactions and they are not video bytes. DEC-003's rule text,
// DEC-004's retention period, and DEC-007's audience are unset. The document
// served here is a placeholder. It is not legal approval and it does not
// enable public launch.
package moderation

import (
	"encoding/json"
	"os"
	"time"
)

// ProposedDelistGate is the blueprint's proposed bound for cooperating
// services this operator controls. It is not a promise and not a measured
// service level outside a test that injects its own clock.
const ProposedDelistGate = 5 * time.Minute

// Policy is the document a client may show. LegalApproval is always false.
// PublicLaunch is always DISABLED. A file cannot change those two fields.
type Policy struct {
	PublicLaunch                string            `json:"public_launch"`
	LegalApproval               bool              `json:"legal_approval"`
	PolicyStatus                string            `json:"policy_status"`
	RuleText                    string            `json:"rule_text"`
	AppealAuthority             string            `json:"appeal_authority"`
	Contacts                    map[string]string `json:"contacts"`
	Retention                   Retention         `json:"retention"`
	Audience                    Audience          `json:"audience"`
	FiveMinuteGate              string            `json:"five_minute_gate"`
	ValidatorsDecideLegality    bool              `json:"validators_decide_legality"`
	ChainCanOverrideLocalDelist bool              `json:"chain_can_override_local_delist"`
	IncidentReviewApproved      bool              `json:"incident_review_approved"`
	RightsReviewApproved        bool              `json:"rights_review_approved"`
	ContactOverrideIgnored      bool              `json:"contact_file_cannot_grant_approval"`
	DecisionIDs                 []string          `json:"decision_ids"`
}

// Retention states what this build keeps while DEC-004 sets no duration.
type Retention struct {
	DecisionID        string   `json:"decision_id"`
	Duration          any      `json:"duration"`
	Kept              string   `json:"kept"`
	DroppedOnDecision string   `json:"dropped_on_decision"`
	NotErased         []string `json:"not_erased"`
}

// Audience states that DEC-007 names no community and no age rule.
type Audience struct {
	DecisionID       string `json:"decision_id"`
	AgeRule          string `json:"age_rule"`
	MinorSafetyClaim bool   `json:"minor_safety_claim"`
	PublicSignup     bool   `json:"public_signup"`
}

// Document is the placeholder. It does not name a person.
func Document() Policy {
	return Policy{
		PublicLaunch:    "DISABLED",
		LegalApproval:   false,
		PolicyStatus:    "PLACEHOLDER",
		RuleText:        "PLACEHOLDER. No community rule text is approved. DR-23-01.",
		AppealAuthority: "PLACEHOLDER. No appeal authority is named. DR-23-07.",
		Contacts: map[string]string{
			"trust_safety":   "PLACEHOLDER unassigned",
			"product":        "PLACEHOLDER unassigned",
			"chain_security": "PLACEHOLDER unassigned",
			"media":          "PLACEHOLDER unassigned",
			"deployment":     "PLACEHOLDER unassigned",
		},
		Retention: Retention{
			DecisionID:        "DEC-004",
			Duration:          nil,
			Kept:              "Minimum only: decision outcome, reason code, admin audit row, and delist acknowledgements. No duration is set.",
			DroppedOnDecision: "Complaint narrative and appeal statement are dropped when that step is decided.",
			NotErased:         []string{"independent copies", "historical ledger records"},
		},
		Audience: Audience{
			DecisionID:       "DEC-007",
			AgeRule:          "UNSET",
			MinorSafetyClaim: false,
			PublicSignup:     false,
		},
		FiveMinuteGate:              "PROPOSED",
		ValidatorsDecideLegality:    false,
		ChainCanOverrideLocalDelist: false,
		IncidentReviewApproved:      false,
		RightsReviewApproved:        false,
		ContactOverrideIgnored:      true,
		DecisionIDs:                 []string{"DEC-003", "DEC-004", "DEC-007"},
	}
}

// LoadContacts reads an operator contact file and returns the placeholder
// document with those contact strings copied in. legal_approval and
// public_launch in the file are ignored.
func LoadContacts(path string) (Policy, error) {
	doc := Document()
	if path == "" {
		return doc, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return doc, err
	}
	var file struct {
		Contacts      map[string]string `json:"contacts"`
		LegalApproval *bool             `json:"legal_approval"`
		PublicLaunch  string            `json:"public_launch"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return doc, err
	}
	for key, value := range file.Contacts {
		if value == "" || len(value) > 80 {
			continue
		}
		doc.Contacts[key] = value
	}
	doc.LegalApproval = false
	doc.PublicLaunch = "DISABLED"
	doc.ContactOverrideIgnored = true
	_ = file.LegalApproval
	return doc, nil
}
