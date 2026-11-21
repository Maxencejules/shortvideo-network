package obs

// Rates are operator inputs. A nil pointer means the rate was not supplied.
// A supplied zero is an input, not a default invented here.
type Rates struct {
	Currency             string
	TransferMinorPerByte *int64
	StorageMinorPerByte  *int64
}

// Worksheet separates measured units from rates. MonthlyBill is always nil.
type Worksheet struct {
	ThresholdLabel        string           `json:"threshold_label"`
	CurrencyPreference    string           `json:"currency_preference"`
	CurrencyApproved      bool             `json:"currency_approved"`
	MeasuredDelivered     map[string]int64 `json:"measured_delivered_bytes"`
	MeasuredStorage       map[string]int64 `json:"measured_storage_bytes"`
	NetworkBytes          int64            `json:"network_bytes"`
	RatesSupplied         bool             `json:"rates_supplied"`
	MonthlyBill           *int64           `json:"monthly_bill"`
	PeriodProduct         *int64           `json:"period_product_minor_units"`
	PeriodProductLabel    string           `json:"period_product_label"`
	ForecastBlockedReason string           `json:"forecast_blocked_reason"`
	FounderCashSupplied   bool             `json:"founder_cash_supplied"`
	FounderCashMinor      *int64           `json:"founder_cash_minor_units"`
	VolunteerSupplied     bool             `json:"volunteer_supplied"`
	VolunteerNote         string           `json:"volunteer_note"`
	Unmeasured            []string         `json:"unmeasured"`
}

// Usage is the auditable report. It does not forecast a month.
type Usage struct {
	BudgetOwner        string           `json:"budget_owner"`
	OnCall             string           `json:"on_call"`
	Coverage           string           `json:"coverage"`
	PublicLaunch       bool             `json:"public_launch"`
	TradableAsset      bool             `json:"tradable_asset"`
	AlertsAreSpendCap  bool             `json:"alerts_are_spend_cap"`
	ThresholdLabel     string           `json:"threshold_label"`
	ChainSubmissions   int              `json:"chain_submissions"`
	WatchEventsOnChain int              `json:"watch_events_on_chain"`
	Denials            map[string]int64 `json:"denials"`
	DeliveredBytes     map[string]int64 `json:"delivered_bytes"`
	StorageBytes       map[string]int64 `json:"storage_bytes"`
	Decisions          []string         `json:"decisions"`
}

// Panel is one dashboard row. Assigned is false until a person is named.
type Panel struct {
	Name            string `json:"name"`
	Metric          string `json:"metric"`
	Alert           string `json:"alert"`
	Runbook         string `json:"runbook"`
	RolePlaceholder string `json:"role_placeholder"`
	Assigned        bool   `json:"assigned"`
	Active          bool   `json:"active"`
}

// OpenDecisions are the goal 25 requests that still need an owner.
func OpenDecisions() []string {
	return []string{
		"DR-25-01", "DR-25-02", "DR-25-03", "DR-25-04", "DR-25-05",
		"DR-25-06", "DR-25-07", "DR-25-08", "DR-25-09", "DR-25-10",
		"DR-25-11", "DR-25-12",
	}
}

// SetRates stores an operator rate card. It does not invent a missing rate.
func (s *Service) SetRates(r Rates) error {
	if r.TransferMinorPerByte != nil && *r.TransferMinorPerByte < 0 {
		return ErrUnits
	}
	if r.StorageMinorPerByte != nil && *r.StorageMinorPerByte < 0 {
		return ErrUnits
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rates = r
	return nil
}

// SetFounderCash records an operator-supplied amount. It is not added to
// network bytes and it is not a rate.
func (s *Service) SetFounderCash(minor int64) error {
	if minor < 0 {
		return ErrUnits
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.founderSet = true
	s.founderMinor = minor
	return nil
}

// SetVolunteerNote records a label. It is not converted into a cash figure.
func (s *Service) SetVolunteerNote(note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.volunteer = clip(redact(s.token, note))
}

// Worksheet copies the measured units and leaves the monthly bill empty.
func (s *Service) Worksheet() Worksheet {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivered := copyMap(s.delivered)
	storage := copyMap(s.storage)
	w := Worksheet{
		ThresholdLabel:      LabelPlaceholder,
		CurrencyPreference:  CurrencyPreference,
		CurrencyApproved:    false,
		MeasuredDelivered:   delivered,
		MeasuredStorage:     storage,
		NetworkBytes:        deliveredTotal(delivered),
		MonthlyBill:         nil,
		Unmeasured:          append([]string(nil), Unmeasured...),
		FounderCashSupplied: s.founderSet,
		VolunteerSupplied:   s.volunteer != "",
		VolunteerNote:       s.volunteer,
	}
	if s.founderSet {
		v := s.founderMinor
		w.FounderCashMinor = &v
	}
	complete := s.rates.Currency != "" && s.rates.TransferMinorPerByte != nil && s.rates.StorageMinorPerByte != nil
	w.RatesSupplied = complete
	if !complete {
		if s.rates.Currency == "" && s.rates.TransferMinorPerByte == nil && s.rates.StorageMinorPerByte == nil {
			w.ForecastBlockedReason = "rates_not_supplied"
		} else {
			w.ForecastBlockedReason = "rates_incomplete"
		}
		w.PeriodProductLabel = ""
		return w
	}
	transfer := deliveredTotal(delivered) * *s.rates.TransferMinorPerByte
	stored := deliveredTotal(storage) * *s.rates.StorageMinorPerByte
	product := transfer + stored
	w.PeriodProduct = &product
	w.PeriodProductLabel = "not_a_monthly_forecast"
	w.ForecastBlockedReason = ""
	return w
}

// UsageReport is the auditable summary for the recorded readings.
func (s *Service) UsageReport() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Usage{
		BudgetOwner:        BudgetOwnerUnassigned,
		OnCall:             BudgetOwnerUnassigned,
		Coverage:           BudgetOwnerUnassigned,
		PublicLaunch:       false,
		TradableAsset:      false,
		AlertsAreSpendCap:  false,
		ThresholdLabel:     LabelPlaceholder,
		ChainSubmissions:   s.chainWrites,
		WatchEventsOnChain: 0,
		Denials:            copyMap(s.denials),
		DeliveredBytes:     copyMap(s.delivered),
		StorageBytes:       copyMap(s.storage),
		Decisions:          OpenDecisions(),
	}
}

// Dashboard lists the alert and runbook mapping. No role is assigned.
func (s *Service) Dashboard() []Panel {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []Panel{
		{Name: "Commit age", Metric: "commit_age_seconds", Alert: "stale_commit", Runbook: "RB-STALE-COMMIT", RolePlaceholder: "UNASSIGNED chain/security"},
		{Name: "Quorum", Metric: "quorum_lost", Alert: "quorum_loss", Runbook: "RB-QUORUM", RolePlaceholder: "UNASSIGNED chain/security"},
		{Name: "Index lag", Metric: "index_lag_heights", Alert: "index_lag", Runbook: "RB-INDEX-LAG", RolePlaceholder: "UNASSIGNED deployment/on-call"},
		{Name: "Replicas", Metric: "reachable_replicas", Alert: "replicas", Runbook: "RB-REPLICAS", RolePlaceholder: "UNASSIGNED media/provider agents"},
		{Name: "Repair backlog", Metric: "repair_backlog", Alert: "repair_backlog", Runbook: "RB-REPAIR", RolePlaceholder: "UNASSIGNED media/provider agents"},
		{Name: "Processing age", Metric: "processing_age_seconds", Alert: "processing_age", Runbook: "RB-PROCESSING", RolePlaceholder: "UNASSIGNED media/provider agents"},
		{Name: "Disk free", Metric: "disk_free_bytes", Alert: "low_disk", Runbook: "RB-DISK", RolePlaceholder: "UNASSIGNED deployment/on-call"},
		{Name: "Playback", Metric: "playback_failures", Alert: "playback_errors", Runbook: "RB-PLAYBACK", RolePlaceholder: "UNASSIGNED product/application"},
		{Name: "Reports", Metric: "unreviewed_reports", Alert: "unreviewed_reports", Runbook: "RB-REPORTS", RolePlaceholder: "UNASSIGNED trust/safety"},
		{Name: "Admission", Metric: "denials", Alert: "admission", Runbook: "RB-ADMISSION", RolePlaceholder: "UNASSIGNED deployment/on-call"},
	}
	for i := range rows {
		rows[i].Active = s.alerts[rows[i].Alert]
		rows[i].Assigned = false
	}
	return rows
}

// ActiveAlerts returns the alert names that are on.
func (s *Service) ActiveAlerts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedKeys(s.alerts)
}
