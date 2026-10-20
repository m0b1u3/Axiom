package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrSettingsConflict = errors.New("settings changed; reload before saving")

type QueueSettings struct {
	Global     int       `json:"global"`
	PerModel   int       `json:"perModel"`
	PerProject int       `json:"perProject"`
	Revision   int       `json:"revision"`
	UpdatedAt  time.Time `json:"updatedAt"`
	UpdatedBy  string    `json:"updatedBy"`
}

func (v QueueSettings) Valid() bool {
	return v.Global >= 1 && v.Global <= 64 && v.PerModel >= 1 && v.PerModel <= 64 && v.PerProject >= 1 && v.PerProject <= 64
}
func readSettings(ctx context.Context, tx *transaction, defaults ClaimLimits) (QueueSettings, error) {
	var v QueueSettings
	// Retained solely for historical readers. New services no longer configure quotas.
	if defaults.Global < 1 || defaults.Global > 64 {
		defaults.Global = 2
	}
	if defaults.PerModel < 1 || defaults.PerModel > 64 {
		defaults.PerModel = 2
	}
	if defaults.PerProject < 1 || defaults.PerProject > 64 {
		defaults.PerProject = 2
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO execution_settings(id,global_limit,model_limit,project_limit) VALUES(1,?,?,?) ON CONFLICT(id) DO NOTHING`, defaults.Global, defaults.PerModel, defaults.PerProject)
	if e != nil {
		return v, e
	}
	e = tx.QueryRowContext(ctx, `SELECT global_limit,model_limit,project_limit,revision,updated_at,updated_by FROM execution_settings WHERE id=1`).Scan(&v.Global, &v.PerModel, &v.PerProject, &v.Revision, &v.UpdatedAt, &v.UpdatedBy)
	return v, e
}
func (s *Store) QueueSettings(ctx context.Context, defaults ClaimLimits) (QueueSettings, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return QueueSettings{}, e
	}
	defer tx.Rollback()
	v, e := readSettings(ctx, tx, defaults)
	if e != nil {
		return v, e
	}
	return v, tx.Commit()
}

// SaveQueueSettings retains legacy import/fixture support. These values never
// govern admission; the public mutation endpoint is retired.
func (s *Store) SaveQueueSettings(ctx context.Context, v QueueSettings, actor string) (QueueSettings, error) {
	if !v.Valid() {
		return v, errors.New("limits must be integers between 1 and 64")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return v, e
	}
	defer tx.Rollback()
	// Keep the historical record and revision atomic for legacy readers.
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(740919)`); e != nil {
		return v, e
	}
	e = tx.QueryRowContext(ctx, `UPDATE execution_settings SET global_limit=?,model_limit=?,project_limit=?,revision=revision+1,updated_at=NOW(),updated_by=? WHERE id=1 AND revision=? RETURNING revision,updated_at,updated_by`, v.Global, v.PerModel, v.PerProject, actor, v.Revision).Scan(&v.Revision, &v.UpdatedAt, &v.UpdatedBy)
	if errors.Is(e, sql.ErrNoRows) {
		return v, ErrSettingsConflict
	}
	if e != nil {
		return v, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO execution_setting_changes(revision,global_limit,model_limit,project_limit,updated_by) VALUES(?,?,?,?,?)`, v.Revision, v.Global, v.PerModel, v.PerProject, actor); e != nil {
		return v, e
	}
	return v, tx.Commit()
}
func (s *Store) QueueSettingHistory(ctx context.Context) ([]QueueSettings, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT global_limit,model_limit,project_limit,revision,updated_at,updated_by FROM execution_setting_changes ORDER BY revision DESC LIMIT 20`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []QueueSettings{}
	for rows.Next() {
		var v QueueSettings
		if e = rows.Scan(&v.Global, &v.PerModel, &v.PerProject, &v.Revision, &v.UpdatedAt, &v.UpdatedBy); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type AgentPresence struct {
	DispatchMode            string                            `json:"dispatchMode"`
	RuntimeReportedAt       *time.Time                        `json:"runtimeReportedAt,omitempty"`
	Runtime                 *contracts.AgentRuntime           `json:"runtime,omitempty"`
	ObservationCapabilities contracts.ObservationCapabilities `json:"observationCapabilities"`
	ResultCapabilities      contracts.ResultCapabilities      `json:"resultCapabilities"`
	InputCapabilities       contracts.InputCapabilities       `json:"inputCapabilities"`
	AgentID                 string                            `json:"agentId"`
	Slots                   int                               `json:"slots"`
	LastSeen                time.Time                         `json:"lastSeen"`
	Online                  bool                              `json:"online"`
}

func (s *Store) ReportAgent(ctx context.Context, id string, slots int) error {
	return s.ReportAgentWithInput(ctx, id, slots, contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{}, JobVersions: []contracts.SchemaVersion{}})
}

func (s *Store) ReportAgentWithInput(ctx context.Context, id string, slots int, c contracts.InputCapabilities) error {
	return s.ReportAgentWithContracts(ctx, id, slots, c, contracts.NoResultCapabilities())
}
func (s *Store) ReportAgentWithContracts(ctx context.Context, id string, slots int, c contracts.InputCapabilities, results contracts.ResultCapabilities) error {
	return s.ReportAgentWithObservations(ctx, id, slots, c, results, contracts.NoObservationCapabilities())
}
func (s *Store) ReportAgentWithObservations(ctx context.Context, id string, slots int, c contracts.InputCapabilities, results contracts.ResultCapabilities, observations contracts.ObservationCapabilities) error {
	return s.ReportAgentWithDispatch(ctx, id, control.DispatchBounded, slots, c, results, observations)
}
func (s *Store) ReportAgentWithDispatch(ctx context.Context, id, mode string, slots int, c contracts.InputCapabilities, results contracts.ResultCapabilities, observations contracts.ObservationCapabilities) error {
	if !control.ValidDispatchPresence(mode, slots) {
		return errors.New("invalid execution dispatch mode")
	}
	raw, e := json.Marshal(c)
	if e != nil {
		return e
	}
	if e = contracts.ValidateJSON(contracts.KindInputCapabilities, raw, nil); e != nil {
		return e
	}
	output, e := json.Marshal(results)
	if e != nil {
		return e
	}
	if e = contracts.ValidateJSON(contracts.KindResultCapabilities, output, nil); e != nil {
		return e
	}
	observed, e := contracts.EncodeJSON(observations)
	if e != nil {
		return e
	}
	if e = contracts.ValidateJSON(contracts.KindObservationCapabilities, observed, nil); e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO execution_agents(agent_id,slots,last_seen,input_capabilities_json,result_capabilities_json,observation_capabilities_json) VALUES(?,?,NOW(),?,?,?) ON CONFLICT(agent_id) DO UPDATE SET slots=EXCLUDED.slots,last_seen=NOW(),input_capabilities_json=EXCLUDED.input_capabilities_json,result_capabilities_json=EXCLUDED.result_capabilities_json,observation_capabilities_json=EXCLUDED.observation_capabilities_json`, id, slots, string(raw), string(output), string(observed))
	return e
}

type QueueTask struct {
	sdkConcurrency     bool
	sdkOperation       bool
	scanInput          bool
	sdkParameters      bool
	TaskID             int64              `json:"taskId"`
	Status             string             `json:"status"`
	AgentID            string             `json:"agentId"`
	QueuedAt           time.Time          `json:"queuedAt"`
	StartedAt          string             `json:"startedAt"`
	FinishedAt         string             `json:"finishedAt"`
	HeartbeatAt        string             `json:"heartbeatAt"`
	LeaseExpiresAt     string             `json:"leaseExpiresAt"`
	Position           int                `json:"position"`
	Reason             string             `json:"reason"`
	Compatibility      *TaskCompatibility `json:"compatibility,omitempty"`
	AutomaticRetries   int                `json:"automaticRetries"`
	modelID            int64
	projectID          int64
	provider           string
	inputVersion       int
	resultVersion      int
	observationVersion int
	budgetTotal        *int64
	budgetUnknown      bool
	budgetUsed         int64
	budget             bool
}
type QueueOverview struct {
	DispatchMode      string          `json:"dispatchMode"`
	OnlineAgents      int             `json:"onlineAgents"`
	ReadyAgents       int             `json:"readyAgents"`
	Settings          QueueSettings   `json:"settings"`
	Agents            []AgentPresence `json:"agents"`
	Tasks             []QueueTask     `json:"tasks"`
	Active            int             `json:"active"`
	Pending           int             `json:"pending"`
	OnlineSlots       int             `json:"onlineSlots"`
	ReadySlots        int             `json:"readySlots"`
	EffectiveCapacity *int            `json:"effectiveCapacity"`
	ServerTime        time.Time       `json:"serverTime"`
	MaxRecoveries     int             `json:"maxRecoveries"`
}

func (s *Store) QueueOverview(ctx context.Context, defaults ClaimLimits, taskID int64) (QueueOverview, error) {
	v := QueueOverview{DispatchMode: control.DispatchUnbounded, Agents: []AgentPresence{}, Tasks: []QueueTask{}, ServerTime: time.Now().UTC(), MaxRecoveries: defaults.MaxRecoveries}
	settings, e := s.QueueSettings(ctx, defaults)
	if e != nil {
		return v, e
	}
	v.Settings = settings
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return v, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT agent_id,slots,last_seen,input_capabilities_json,result_capabilities_json,observation_capabilities_json,runtime_json,runtime_seen_at FROM execution_agents ORDER BY agent_id`)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var a AgentPresence
		var raw, output, observed, runtimeJSON string
		if e = rows.Scan(&a.AgentID, &a.Slots, &a.LastSeen, &raw, &output, &observed, &runtimeJSON, &a.RuntimeReportedAt); e != nil {
			rows.Close()
			return v, e
		}
		_ = json.Unmarshal([]byte(raw), &a.InputCapabilities)
		_ = json.Unmarshal([]byte(output), &a.ResultCapabilities)
		_ = json.Unmarshal([]byte(observed), &a.ObservationCapabilities)
		// Presence from an older Agent must not keep a replaced component's
		// runtime diagnostic alive indefinitely under the same Agent ID.
		if runtimeJSON != "" && a.RuntimeReportedAt != nil && v.ServerTime.Sub(*a.RuntimeReportedAt) < 30*time.Second {
			if runtime, err := contracts.DecodeAgentRuntime([]byte(runtimeJSON), nil); err == nil {
				a.Runtime = &runtime
			}
		}
		a.Online = v.ServerTime.Sub(a.LastSeen) < 30*time.Second
		a.DispatchMode = control.DispatchBounded
		if a.Slots == 0 {
			a.DispatchMode = control.DispatchUnbounded
		}
		if a.Online {
			v.OnlineAgents++
			v.OnlineSlots += a.Slots
			if a.Runtime == nil || (a.Runtime.State == contracts.AgentRuntimeStateReady && a.Runtime.MatrixHash == control.MatrixHash) {
				v.ReadyAgents++
				v.ReadySlots += a.Slots
			}
		}
		v.Agents = append(v.Agents, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT `+s.needsSDKConcurrencySQL("scan_tasks")+`,COALESCE(sdk_parameters_json,'')<>'',EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id),EXISTS(SELECT 1 FROM task_scan_inputs si WHERE si.task_id=scan_tasks.id),id,status,COALESCE(agent_id,''),queued_at,COALESCE(started_at,''),COALESCE(finished_at,''),COALESCE(heartbeat_at,''),COALESCE(lease_expires_at,''),automatic_retries,COALESCE(model_config_id,0),COALESCE(project_id,0),provider,input_contract_version,result_contract_version,observation_contract_version,(max_cost_usd IS NOT NULL OR EXISTS(SELECT 1 FROM task_budgets b WHERE b.task_id=scan_tasks.id AND b.total_micros IS NOT NULL)),(SELECT total_micros FROM task_budgets b WHERE b.task_id=scan_tasks.id),EXISTS(SELECT 1 FROM task_cost_attempts c WHERE c.task_id=scan_tasks.id AND c.state='unknown'),(SELECT LEAST(COALESCE(SUM(c.booked_micros),0),9007199254740991)::bigint FROM task_cost_attempts c WHERE c.task_id=scan_tasks.id) FROM scan_tasks WHERE status IN ('pending','claimed','scanning','cancel_requested') OR id=? ORDER BY queued_at,id`, taskID)
	if e != nil {
		return v, e
	}
	all := []QueueTask{}
	for rows.Next() {
		var t QueueTask
		if e = rows.Scan(&t.sdkConcurrency, &t.sdkParameters, &t.sdkOperation, &t.scanInput, &t.TaskID, &t.Status, &t.AgentID, &t.QueuedAt, &t.StartedAt, &t.FinishedAt, &t.HeartbeatAt, &t.LeaseExpiresAt, &t.AutomaticRetries, &t.modelID, &t.projectID, &t.provider, &t.inputVersion, &t.resultVersion, &t.observationVersion, &t.budget, &t.budgetTotal, &t.budgetUnknown, &t.budgetUsed); e != nil {
			rows.Close()
			return v, e
		}
		all = append(all, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	stamp := v.ServerTime.Format(time.RFC3339)
	if s.IsPostgres() {
		// Identity operations are visible work, independently of scan tasks.
		var identities int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM sdk_identity_operations WHERE status IN ('running','waiting') AND lease_until>=? AND deadline>=?`, stamp, stamp).Scan(&identities); e != nil {
			return v, e
		}
		v.Active += identities
	}
	for _, t := range all {
		if t.Status != "pending" && IsRunningStatus(t.Status) && t.LeaseExpiresAt >= stamp {
			v.Active++
		}
	}
	storageReason := ""
	if defaults.StorageCheck != nil {
		storageReason = defaults.StorageCheck(ctx)
	}
	for _, t := range all {
		if t.Status == "pending" {
			t.Compatibility = evaluateTaskCompatibility(t, v.Agents)
			v.Pending++
			t.Position = v.Pending
			switch {
			case t.budgetTotal != nil && t.budgetUnknown:
				t.Reason = "cost_unknown"
			case t.budgetTotal != nil && t.budgetUsed >= *t.budgetTotal:
				t.Reason = "budget_exhausted"
			case storageReason != "":
				t.Reason = storageReason
			case v.OnlineAgents == 0:
				t.Reason = "agent_offline"
			case t.Compatibility.Reason != "":
				t.Reason = t.Compatibility.Reason
			default:
				t.Reason = "awaiting_dispatch"
			}
		} else if IsRunningStatus(t.Status) && t.LeaseExpiresAt < stamp {
			t.Reason = "lease_expired"
		} else if t.Status == "cancel_requested" {
			t.Reason = "cancel_requested"
		}
		if taskID == 0 || taskID == t.TaskID {
			v.Tasks = append(v.Tasks, t)
		}
	}
	return v, tx.Commit()
}

type AttemptResult struct {
	ContractVersion int                            `json:"contractVersion"`
	Completion      *contracts.ExecutionCompletion `json:"completion"`
	Result          *contracts.ScanResult          `json:"result"`
	ArchiveSHA256   string                         `json:"archiveSha256,omitempty"`
	Coverage        json.RawMessage                `json:"coverage"`
	Available       bool                           `json:"available"`
	Findings        []Finding                      `json:"findings"`
}

func (s *Store) AttemptResult(ctx context.Context, id int64, attempt int) (AttemptResult, error) {
	v := AttemptResult{Findings: []Finding{}}
	var raw sql.NullString
	e := s.db.QueryRowContext(ctx, `SELECT result_json,result_contract_version FROM scan_attempts WHERE task_id=? AND attempt=?`, id, attempt).Scan(&raw, &v.ContractVersion)
	if e != nil {
		return v, e
	}
	if !raw.Valid {
		return v, nil
	}
	digest := sha256.Sum256([]byte(raw.String))
	v.ArchiveSHA256 = hex.EncodeToString(digest[:])
	if v.ContractVersion == 1 {
		expected := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(attempt)}
		full, err := contracts.DecodeExecutionCompletion([]byte(raw.String), &expected)
		if err != nil {
			return v, err
		}
		v.Available = true
		v.Completion = &full
		if full.Result.Present {
			r := full.Result.Value
			v.Result = &r
			details := map[string]json.RawMessage{}
			for k, x := range r.Coverage.Details {
				details[k] = x
			}
			details["completeness"], _ = json.Marshal(r.Coverage.Completeness)
			v.Coverage, _ = json.Marshal(details)
			for _, f := range r.Findings {
				v.Findings = append(v.Findings, projectFinding(id, f))
			}
		}
		return v, nil
	}
	var c Completion
	if e = json.Unmarshal([]byte(raw.String), &c); e != nil {
		return v, e
	}
	v.Available = true
	v.Coverage = c.Coverage
	if c.Findings != nil {
		v.Findings = c.Findings
	}
	return v, nil
}
