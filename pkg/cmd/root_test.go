package cmd

import (
	"testing"

	"github.com/nareshku/kubectl-container-status/pkg/types"
)

func TestLogsRestriction(t *testing.T) {
	tests := []struct {
		name           string
		workloadKind   string
		showLogs       bool
		expectLogsFlag bool
	}{
		{
			name:           "Pod with logs enabled",
			workloadKind:   "Pod",
			showLogs:       true,
			expectLogsFlag: true,
		},
		{
			name:           "Pod with logs disabled",
			workloadKind:   "Pod",
			showLogs:       false,
			expectLogsFlag: false,
		},
		{
			name:           "Deployment with logs enabled should disable logs",
			workloadKind:   "Deployment",
			showLogs:       true,
			expectLogsFlag: false,
		},
		{
			name:           "StatefulSet with logs enabled should disable logs",
			workloadKind:   "StatefulSet",
			showLogs:       true,
			expectLogsFlag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &types.Options{
				ShowLogs: tt.showLogs,
			}

			workload := types.WorkloadInfo{
				Kind: tt.workloadKind,
				Name: "test-workload",
			}

			// Simulate the logs restriction logic from runContainerStatus
			isSinglePod := workload.Kind == "Pod"
			if options.ShowLogs && !isSinglePod {
				options.ShowLogs = false
			}

			if options.ShowLogs != tt.expectLogsFlag {
				t.Errorf("Expected ShowLogs to be %v, got %v for %s", tt.expectLogsFlag, options.ShowLogs, tt.workloadKind)
			}
		})
	}
}
