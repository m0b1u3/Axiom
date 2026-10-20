package worker

import (
	"axiom/code-audit-agent/internal/scanner"
	"axiom/code-audit-agent/pkg/execution"
	"axiom/contracts"
	"context"
	"errors"
	"sync"
	"time"
)

func (w *Worker) executeIdentity(ctx context.Context, c *execution.Client) (bool, error) {
	claim, e := c.ClaimSDKIdentity(ctx, w.Cfg.AgentID)
	if e != nil || claim == nil {
		return false, e
	}
	return w.executeIdentityClaim(ctx, c, claim)
}

func (w *Worker) executeIdentityClaim(ctx context.Context, c *execution.Client, claim *contracts.SDKIdentityClaim) (bool, error) {
	deadline, e := time.Parse(time.RFC3339, claim.Operation.Deadline)
	if e != nil {
		return true, e
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	latest := contracts.SDKIdentityReport{SchemaVersion: 1, OperationID: claim.Operation.ID, IdentityID: claim.Operation.Identity.ID, AgentID: w.Cfg.AgentID, Generation: claim.Operation.Identity.Generation, LeaseToken: claim.LeaseToken, Sequence: 1, Status: "running"}
	var mu sync.Mutex
	lastOK := time.Now()
	lease := time.Duration(claim.LeaseSeconds) * time.Second
	interval, reserve := renewalTiming(lease)
	stopped := make(chan struct{})
	done := make(chan struct{})
	// Identity-specific exclusion and storage admission remain in the Server.
	// A lost lease stops the native login; it is never auto-replayed.
	send := func(v contracts.SDKIdentityReport) error {
		// Use the Server's lease, not local configuration. A blocked request must
		// leave enough time to cancel the SDK before another operation can start.
		reportCtx, stopReport := context.WithDeadline(runCtx, lastOK.Add(lease-reserve))
		defer stopReport()
		status, e := c.ReportSDKIdentity(reportCtx, v)
		if e == nil {
			lastOK = time.Now()
			if status == "canceled" || status == "expired" {
				cancel()
				return errors.New("identity operation canceled")
			}
		}
		return e
	}
	if e = send(latest); e != nil {
		return true, e
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				_ = send(latest)
				if time.Since(lastOK) >= lease-reserve {
					cancel()
				}
				mu.Unlock()
			}
		}
	}()
	err := scanner.RunSDKIdentity(runCtx, *claim, w.Cfg.DataDir, w.Cfg.ScannerDir, w.Cfg.NodePath, func(v contracts.SDKIdentityReport) error { mu.Lock(); defer mu.Unlock(); latest = v; return send(v) })
	close(stopped)
	<-done
	if err != nil {
		mu.Lock()
		latest.Sequence++
		latest.Status = "failed"
		latest.Instructions = nil
		latest.Authenticated = nil
		message := "SDK 身份操作中断，请在 Web 核对状态"
		latest.Error = &message
		if runCtx.Err() != nil {
			latest.Status = "canceled"
		}
		mu.Unlock()
		finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = c.ReportSDKIdentity(finish, latest)
	}
	return true, err
}
