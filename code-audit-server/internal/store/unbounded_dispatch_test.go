package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPostgresUnboundedDispatchIgnoresAllHistoricalQuotas(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	limits := ClaimLimits{Global: 1, PerModel: 1, PerProject: 1, Lease: time.Minute, OutputRoot: t.TempDir()}
	cfg, err := s.QueueSettings(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveQueueSettings(ctx, cfg, "fixture-history"); err != nil {
		t.Fatal(err)
	}
	user, err := s.EnsureUser(ctx, "fixture-unbounded", "fixture-not-login")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, "Fixtures / Mock shared project", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateModelConfig(ctx, ModelConfigRow{Name: "Fixtures / Mock shared model", Provider: "openai", Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	const count = 70
	for i := 0; i < count; i++ {
		if _, err = s.InsertPending(ctx, Task{ScanNo: fmt.Sprint("fixture-unbounded-", i), SourcePath: "/fixture-not-executed", Provider: "openai", Model: "fixture-model", ScanMode: "standard", ModelConfigID: model.ID, ProjectID: project.ID}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	claimed := make(chan int64, count)
	failures := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task, e := s.ClaimExecution(ctx, fmt.Sprint("fixture-", i), limits)
			if e != nil {
				failures <- e
				return
			}
			if task != nil {
				claimed <- task.ID
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	close(claimed)
	for e := range failures {
		t.Fatal(e)
	}
	if len(claimed) != count {
		t.Fatalf("historical quota blocked work: claimed %d of %d", len(claimed), count)
	}
	seen := map[int64]bool{}
	for id := range claimed {
		if seen[id] {
			t.Fatal("duplicate claim", id)
		}
		seen[id] = true
	}
	if err = s.ReportAgentWithDispatch(ctx, "fixture-unbounded", control.DispatchUnbounded, 0, contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{}, JobVersions: []contracts.SchemaVersion{}}, contracts.NoResultCapabilities(), contracts.NoObservationCapabilities()); err != nil {
		t.Fatal(err)
	}
	v, err := s.QueueOverview(ctx, limits, 0)
	if err != nil || v.Active != count || v.DispatchMode != control.DispatchUnbounded || v.EffectiveCapacity != nil || v.OnlineAgents != 1 || v.ReadyAgents != 1 || v.Agents[0].DispatchMode != control.DispatchUnbounded {
		t.Fatal("unbounded snapshot misrepresented", err, v)
	}
	if err = s.ReportAgentWithObservations(ctx, "invalid-zero", 0, contracts.InputCapabilities{}, contracts.NoResultCapabilities(), contracts.NoObservationCapabilities()); err == nil {
		t.Fatal("implicit zero-slot mode accepted")
	}
	history, err := s.QueueSettingHistory(ctx)
	if err != nil || len(history) != 1 || history[0].Global != 1 {
		t.Fatal("historical settings were lost", err, history)
	}
}
