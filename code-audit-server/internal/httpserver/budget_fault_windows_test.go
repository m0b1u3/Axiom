//go:build windows

package httpserver_test

import (
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"testing"
)

func runBudgetProcessFault(t *testing.T, s *store.Store, server *httpserver.Server, agent, root, url, agentToken, scratch string, create func(float64) int64) {
	t.Log("Windows forced process-tree termination remains a separate acceptance item")
}
