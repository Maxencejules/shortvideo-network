package dash

// Reading is one dashboard value. Class is configured, measured, pending,
// stale, or absent. Absent means the source did not report it. A missing
// source is not shown as zero.
type Reading struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Value  any    `json:"value"`
	Source string `json:"source"`
	Note   string `json:"note,omitempty"`
}

// Snapshot is the public read model. It carries no admin token and no
// consensus key.
type Snapshot struct {
	ChainSubmitted       bool      `json:"chain_submitted"`
	ConsensusKeyAccepted bool      `json:"consensus_key_accepted"`
	SingleMachineDemo    bool      `json:"single_machine_demo"`
	IndependenceProven   bool      `json:"independence_proven"`
	Roles                []string  `json:"roles"`
	Readings             []Reading `json:"readings"`
}

// CouncilView is the read-only governance section.
type CouncilView struct {
	Proposals            Reading `json:"proposals"`
	Upgrade              Reading `json:"upgrade"`
	Instructions         Reading `json:"upgrade_instructions"`
	ConsensusKeyAccepted bool    `json:"consensus_key_accepted"`
	ChainSubmitted       bool    `json:"chain_submitted"`
}

// OnboardResult is the record of a role choice.
type OnboardResult struct {
	Roles                  []string  `json:"roles"`
	StorageSelected        bool      `json:"storage_selected"`
	ValidatorSelected      bool      `json:"validator_selected"`
	StorageForcesValidator bool      `json:"storage_forces_validator"`
	IndependenceProven     bool      `json:"independence_proven"`
	SingleMachineDemo      bool      `json:"single_machine_demo"`
	FailureDomain          Reading   `json:"failure_domain"`
	Observations           []Reading `json:"observations"`
	ChainSubmitted         bool      `json:"chain_submitted"`
	ConsensusKeyAccepted   bool      `json:"consensus_key_accepted"`
}

// ActionResult is the outcome of an admin action.
type ActionResult struct {
	Error                string         `json:"error,omitempty"`
	Result               string         `json:"result,omitempty"`
	ChainSubmitted       bool           `json:"chain_submitted"`
	ConsensusKeyAccepted bool           `json:"consensus_key_accepted"`
	Detail               map[string]any `json:"detail,omitempty"`
}

// AuditRow is one dangerous-action record. It has no request body and no token.
type AuditRow struct {
	ID             int    `json:"id"`
	At             string `json:"at"`
	Role           string `json:"role"`
	Account        string `json:"account"`
	Action         string `json:"action"`
	Target         string `json:"target"`
	Result         string `json:"result"`
	ChainSubmitted bool   `json:"chain_submitted"`
}

// Proposal is the read-only shape this dashboard displays. Approver key
// bytes are not part of it.
type Proposal struct {
	ID               string `json:"id"`
	State            string `json:"state"`
	Approvals        int    `json:"approvals"`
	ApprovalTarget   int    `json:"approval_target"`
	ExecuteAfterUnix int64  `json:"execute_after_unix"`
	ActivationHeight int64  `json:"activation_height"`
	ParameterKey     string `json:"parameter_key"`
}

// Upgrade is the plan row a configured read returned.
type Upgrade struct {
	Name   string `json:"name"`
	Height int64  `json:"height"`
	State  string `json:"state"`
}

const (
	confirmLimits  = "CHANGE LIMITS"
	confirmStop    = "STOP NEW OFFERS"
	confirmDrain   = "DRAIN"
	confirmHandoff = "HANDOFF"
	confirmRenewal = "REVIEW RENEWAL"
	confirmDisable = "DISABLE ENDPOINT"

	// UpgradeInstructions is configured text. It does not install a binary.
	UpgradeInstructions = "Read Query/Policy from the node. Compare the release digest with the reviewed manifest. Install the reviewed binary beside the running one. Do not copy a validator key onto this page. This dashboard does not accept a consensus private key and does not submit a council message."

	// SoftwareVersion names this development build. It is not a release.
	SoftwareVersion = "development"

	observationNote = "observation of this process, not a universal minimum"
)

var dutyRoles = []string{"storage_delivery", "processing", "account_discovery", "validator"}

var sessionRoles = map[string]bool{"viewer": true, "provider": true, "moderator": true, "council": true}
