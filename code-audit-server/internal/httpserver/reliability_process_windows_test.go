//go:build windows

package httpserver_test

import (
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"testing"
)

func runProcessReliabilityAcceptance(t *testing.T, s *store.Store, server *httpserver.Server, agent, root, url, token, agentToken, scratch string, snapshotID int64, encrypted string) {
	t.Run("reliability-unix-process-groups", func(t *testing.T) { t.Skip("production container acceptance requires Linux process groups") })
}
