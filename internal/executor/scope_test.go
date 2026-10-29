package executor

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
)

// TestScopes_CoverEveryRegisteredAction guards against an action being
// registered without a scope able to run it. Execute denies such an action
// rather than running it unauthorized, so the gap would otherwise surface only
// as a puzzling denial at runtime. The scopes listed here must mirror those
// wired in cmd/server.
func TestScopes_CoverEveryRegisteredAction(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	exec := New(
		logger,
		audit.NewMockLogger(logger),
		NewKubeScope(authorization.New(logger, nil, nil), &fakeClientProvider{}),
		NewAWSScope(authorization.NewAWS(logger, authorization.AWSPolicy{}), &fakeAWSProvider{}),
	)

	names := actions.GetActionsNames()
	if len(names) == 0 {
		t.Fatal("no actions registered")
	}

	for _, name := range names {
		action := actions.CreateAction(name)
		if action == nil {
			t.Fatalf("registered action %q could not be created", name)
		}
		if exec.scopeFor(action) == nil {
			t.Errorf("action %q has no execution scope", name)
		}
	}
}
