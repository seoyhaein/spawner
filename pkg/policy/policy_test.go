package policy_test

import (
	"testing"

	"github.com/seoyhaein/spawner/pkg/policy"
)

func TestDefaultAttemptPolicy_MatchesFastFailRecoveryDefaults(t *testing.T) {
	p := policy.DefaultAttemptPolicy()

	if p.UseNewAttempt(policy.AttemptPhaseInitialSubmit) {
		t.Fatal("initial submit must not allocate a new numbered attempt beyond attempt-1")
	}
	if p.UseNewAttempt(policy.AttemptPhaseRecoveryReplay) {
		t.Fatal("default recovery replay should keep the existing attempt")
	}
	if !p.UseNewAttempt(policy.AttemptPhaseManualRequeue) {
		t.Fatal("default manual requeue should allocate a new attempt")
	}
	if !p.UseNewAttempt(policy.AttemptPhaseAutoRetry) {
		t.Fatal("default auto retry should allocate a new attempt")
	}
}
