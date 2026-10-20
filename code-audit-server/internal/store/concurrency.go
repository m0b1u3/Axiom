package store

import (
	"axiom/contracts"
	"fmt"
)

// Native concurrency values are admitted only to a runtime that explicitly
// implements their wider numeric domain. Read the immutable documents, not the
// effective component count (which is naturally min(workers, selected count)).
func (s *Store) needsSDKConcurrencySQL(alias string) string {
	jsonNumber := func(column, field string) string {
		if s.IsPostgres() {
			return fmt.Sprintf("COALESCE((%s::jsonb->>'%s')::numeric,0)", column, field)
		}
		return fmt.Sprintf("COALESCE(json_extract(%s,'$.%s'),0)", column, field)
	}
	return fmt.Sprintf(`(COALESCE(%[1]s.workers,0)>%[2]d OR %[1]s.deep_max_discovery_runs>%[3]d OR %[1]s.deep_stop_after_no_new>%[3]d
 OR EXISTS(SELECT 1 FROM task_scan_inputs si WHERE si.task_id=%[1]s.id AND %[4]s>%[5]d)
 OR EXISTS(SELECT 1 FROM component_scans cs WHERE cs.task_id=%[1]s.id AND %[6]s>%[7]d))`,
		alias, contracts.SDKLegacyWorkers, contracts.SDKLegacyDeepCounts,
		jsonNumber("si.document_json", "subagents"), contracts.SDKLegacySubagents,
		jsonNumber("cs.document_json", "workers"), contracts.SDKLegacyComponentWorkers)
}
