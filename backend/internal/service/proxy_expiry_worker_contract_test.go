package service

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type proxyExpiryWorkerRepositoryStub struct {
	calls atomic.Int32
}

func (s *proxyExpiryWorkerRepositoryStub) SweepExpiredProxies(context.Context, time.Time) (int64, error) {
	s.calls.Add(1)
	return 0, nil
}

func TestNewProxyExpiryServiceDoesNotStartUntilExplicitlyEnabled(t *testing.T) {
	repo := &proxyExpiryWorkerRepositoryStub{}
	svc := NewProxyExpiryService(repo, 5*time.Millisecond)
	require.NotNil(t, svc)

	time.Sleep(20 * time.Millisecond)
	require.Zero(t, repo.calls.Load(), "constructing or wiring the worker must not mutate proxy/account state")

	svc.Start()
	require.Eventually(t, func() bool { return repo.calls.Load() > 0 }, time.Second, 5*time.Millisecond)
	svc.Stop()
}

func TestProxyExpiryWorkerWiringIsExplicitlyGatedByConfiguration(t *testing.T) {
	content, err := os.ReadFile("wire.go")
	require.NoError(t, err)
	source := strings.ToLower(strings.Join(strings.Fields(string(content)), " "))

	require.Contains(t, source, "newproxyexpiryservice", "the worker must be reachable from the application wiring")
	require.Contains(t, source, "proxyexpiry.enabled", "wiring must read the opt-in gate before starting the write worker")
	require.Contains(t, source, ".start()", "enabled wiring must explicitly start the otherwise inert worker")

	generated, err := os.ReadFile("../../cmd/server/wire_gen.go")
	require.NoError(t, err)
	generatedSource := strings.ToLower(strings.Join(strings.Fields(string(generated)), " "))
	require.Contains(t, generatedSource, "proxyexpiry.stop()", "server cleanup must stop and join the worker before infrastructure shutdown")
}
