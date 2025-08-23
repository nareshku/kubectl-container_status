package output

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nareshku/kubectl-container-status/pkg/types"
	"github.com/olekukonko/tablewriter"
)

func TestCreateProgressBar(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	tests := []struct {
		percentage float64
		expected   string
	}{
		{0.0, "░░░░░░░░░░"},
		{10.0, "▓░░░░░░░░░"},
		{50.0, "▓▓▓▓▓░░░░░"},
		{100.0, "▓▓▓▓▓▓▓▓▓▓"},
		{150.0, "▓▓▓▓▓▓▓▓▓▓"}, // Should cap at 100%
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := formatter.createProgressBar(tt.percentage)
			if result != tt.expected {
				t.Errorf("percentage %.1f: expected %s, got %s", tt.percentage, tt.expected, result)
			}
		})
	}
}

func TestCreateProgressBarNoColor(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: true},
	}

	result := formatter.createProgressBar(75.0)
	expected := "75%"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestFormatDuration(t *testing.T) {
	formatter := &Formatter{}

	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{2 * time.Hour, "2h"},
		{3 * 24 * time.Hour, "3d"},
		{90 * time.Second, "1m"},
		{25 * time.Hour, "1d"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := formatter.formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("duration %v: expected %s, got %s", tt.duration, tt.expected, result)
			}
		})
	}
}

func TestGetReadyCount(t *testing.T) {
	formatter := &Formatter{}

	pod := types.PodInfo{
		Containers: []types.ContainerInfo{
			{Name: "container1", Ready: true},
			{Name: "container2", Ready: false},
			{Name: "container3", Ready: true},
		},
	}

	result := formatter.getReadyCount(pod)
	expected := 2
	if result != expected {
		t.Errorf("expected %d ready containers, got %d", expected, result)
	}
}

func TestSortPods(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{SortBy: string(types.SortByName)},
	}

	pods := []types.PodInfo{
		{Name: "pod-c", Age: 1 * time.Hour},
		{Name: "pod-a", Age: 3 * time.Hour},
		{Name: "pod-b", Age: 2 * time.Hour},
	}

	formatter.sortPods(pods)

	expected := []string{"pod-a", "pod-b", "pod-c"}
	for i, pod := range pods {
		if pod.Name != expected[i] {
			t.Errorf("expected pod %s at position %d, got %s", expected[i], i, pod.Name)
		}
	}
}

func TestSortPodsByAge(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{SortBy: string(types.SortByAge)},
	}

	pods := []types.PodInfo{
		{Name: "pod-new", Age: 1 * time.Hour},
		{Name: "pod-old", Age: 3 * time.Hour},
		{Name: "pod-medium", Age: 2 * time.Hour},
	}

	formatter.sortPods(pods)

	// Should be sorted by age descending (oldest first)
	expected := []string{"pod-old", "pod-medium", "pod-new"}
	for i, pod := range pods {
		if pod.Name != expected[i] {
			t.Errorf("expected pod %s at position %d, got %s", expected[i], i, pod.Name)
		}
	}
}

func TestSortPodsByRestarts(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{SortBy: string(types.SortByRestarts)},
	}

	pods := []types.PodInfo{
		{
			Name: "pod-few-restarts",
			Containers: []types.ContainerInfo{
				{RestartCount: 1},
			},
		},
		{
			Name: "pod-many-restarts",
			Containers: []types.ContainerInfo{
				{RestartCount: 5},
				{RestartCount: 3},
			},
		},
		{
			Name: "pod-no-restarts",
			Containers: []types.ContainerInfo{
				{RestartCount: 0},
			},
		},
	}

	formatter.sortPods(pods)

	// Should be sorted by restart count descending (most restarts first)
	expected := []string{"pod-many-restarts", "pod-few-restarts", "pod-no-restarts"}
	for i, pod := range pods {
		if pod.Name != expected[i] {
			t.Errorf("expected pod %s at position %d, got %s", expected[i], i, pod.Name)
		}
	}
}

func TestGetHealthColor(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	tests := []struct {
		level    string
		hasColor bool
	}{
		{string(types.HealthLevelHealthy), true},
		{string(types.HealthLevelDegraded), true},
		{string(types.HealthLevelCritical), true},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			color := formatter.getHealthColor(tt.level)
			// Just test that we get a color object - actual color testing would require more complex setup
			if color == nil {
				t.Errorf("expected color object for level %s, got nil", tt.level)
			}
		})
	}
}

func TestGetResourceColor(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	tests := []struct {
		percentage float64
		name       string
	}{
		{50.0, "normal usage"},
		{85.0, "high usage"},
		{95.0, "critical usage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color := formatter.getResourceColor(tt.percentage)
			// Just test that we get a color object
			if color == nil {
				t.Errorf("expected color object for percentage %.1f, got nil", tt.percentage)
			}
		})
	}
}

func TestContainerRestartReason(t *testing.T) {
	tests := []struct {
		name              string
		container         types.ContainerInfo
		expectedLastState string
	}{
		{
			name: "container with restart reason",
			container: types.ContainerInfo{
				Name:            "test-container",
				LastState:       "Terminated",
				LastStateReason: "Error",
				RestartCount:    2,
			},
			expectedLastState: "Terminated (Error)",
		},
		{
			name: "container with no restart reason",
			container: types.ContainerInfo{
				Name:         "test-container",
				LastState:    "None",
				RestartCount: 0,
			},
			expectedLastState: "None",
		},
		{
			name: "container with waiting state and reason",
			container: types.ContainerInfo{
				Name:            "test-container",
				LastState:       "Waiting",
				LastStateReason: "ImagePullBackOff",
				RestartCount:    1,
			},
			expectedLastState: "Waiting (ImagePullBackOff)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the last state formatting logic
			lastState := tt.container.LastState
			if tt.container.LastStateReason != "" && tt.container.LastState != "None" {
				lastState = tt.container.LastState + " (" + tt.container.LastStateReason + ")"
			}

			if lastState != tt.expectedLastState {
				t.Errorf("expected last state %s, got %s", tt.expectedLastState, lastState)
			}
		})
	}
}

func TestTableConfiguration(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	// Test terminal width calculation
	terminalWidth := formatter.getTerminalWidth()
	if terminalWidth < 80 {
		t.Errorf("Expected minimum terminal width of 80, got %d", terminalWidth)
	}

	// Test that table configuration methods don't panic
	workload := types.WorkloadInfo{
		Pods: []types.PodInfo{
			{
				Name:     "test-pod-very-long-name-that-might-cause-formatting-issues",
				NodeName: "test-node-with-extremely-long-name-that-exceeds-normal-limits",
				Health:   types.HealthStatus{Level: "Healthy", Reason: "All containers running"},
				Containers: []types.ContainerInfo{
					{Name: "test-container", Ready: true, RestartCount: 0},
				},
			},
		},
	}

	// Test workload table configuration (should not panic)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("configureWorkloadTableWidths panicked: %v", r)
		}
	}()

	// Create a mock table writer to test configuration
	// Note: We can't easily test the actual output without a real terminal,
	// but we can at least verify the methods don't panic
	table := tablewriter.NewWriter(os.Stdout)
	formatter.configureWorkloadTableWidths(table, workload)
	formatter.configureContainerTableWidths(table)
}

func TestTerminalWidthHandling(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	// Test that terminal width detection works
	width := formatter.getTerminalWidth()

	// Should return a reasonable width (minimum 80, default 120 if can't detect)
	if width < 80 {
		t.Errorf("Terminal width too small: %d, expected at least 80", width)
	}

	if width > 500 {
		t.Errorf("Terminal width too large: %d, seems unrealistic", width)
	}
}

func TestLogsWarning(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false, ShowLogs: true},
	}

	// Test that printLogsWarning doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("printLogsWarning panicked: %v", r)
		}
	}()

	// This will print to stdout but shouldn't cause any errors
	formatter.printLogsWarning()
}

func TestServiceAccountDisplay(t *testing.T) {
	tests := []struct {
		name           string
		serviceAccount string
		shouldShow     bool
	}{
		{
			name:           "Custom service account should be shown",
			serviceAccount: "my-custom-sa",
			shouldShow:     true,
		},
		{
			name:           "Default service account should not be shown",
			serviceAccount: "default",
			shouldShow:     false,
		},
		{
			name:           "Empty service account should not be shown",
			serviceAccount: "",
			shouldShow:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := types.PodInfo{
				Name:           "test-pod",
				NodeName:       "test-node",
				ServiceAccount: tt.serviceAccount,
				Age:            time.Hour,
				Health:         types.HealthStatus{Level: "Healthy", Reason: "All containers running"},
			}

			// Test the logic that determines whether to show service account in Pod header
			shouldShow := pod.ServiceAccount != "" && pod.ServiceAccount != "default"
			if shouldShow != tt.shouldShow {
				t.Errorf("Expected shouldShow to be %v for service account '%s', got %v",
					tt.shouldShow, tt.serviceAccount, shouldShow)
			}
		})
	}
}

func TestPodHeaderWithServiceAccount(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	pod := types.PodInfo{
		Name:           "test-pod",
		Status:         "Running",
		NodeName:       "test-node",
		ServiceAccount: "my-custom-sa",
		Age:            time.Hour,
		Health:         types.HealthStatus{Level: "Healthy", Reason: "All containers running"},
	}

	// Test that printPodHeader doesn't panic with service account
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("printPodHeader panicked: %v", r)
		}
	}()

	// This will print to stdout but shouldn't cause any errors
	formatter.printPodHeader(pod)
}

func TestPodConditionsDisplay(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	tests := []struct {
		name       string
		pod        types.PodInfo
		shouldShow bool
	}{
		{
			name: "Pending pod should show conditions",
			pod: types.PodInfo{
				Name:   "pending-pod",
				Status: "Pending",
				Conditions: []types.PodCondition{
					{Type: "PodScheduled", Status: "False", Reason: "Unschedulable"},
				},
			},
			shouldShow: true,
		},
		{
			name: "Running pod with failed condition should show conditions",
			pod: types.PodInfo{
				Name:   "running-pod",
				Status: "Running",
				Conditions: []types.PodCondition{
					{Type: "Ready", Status: "False", Reason: "ContainersNotReady"},
				},
			},
			shouldShow: true,
		},
		{
			name: "Running pod with all true conditions should not show conditions",
			pod: types.PodInfo{
				Name:   "healthy-pod",
				Status: "Running",
				Conditions: []types.PodCondition{
					{Type: "Ready", Status: "True"},
					{Type: "PodScheduled", Status: "True"},
				},
			},
			shouldShow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that printPodConditions doesn't panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("printPodConditions panicked: %v", r)
				}
			}()

			formatter.printPodConditions(tt.pod)
			// Note: We can't easily test the output without capturing stdout,
			// but we can verify the function doesn't panic
		})
	}
}

func TestFailedSchedulingEventPriority(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	events := []types.EventInfo{
		{
			Time:    time.Now().Add(-5 * time.Minute),
			Type:    "Normal",
			Reason:  "Started",
			Message: "Started container",
		},
		{
			Time:    time.Now().Add(-3 * time.Minute),
			Type:    "Warning",
			Reason:  "FailedScheduling",
			Message: "0/46 nodes are available: 1 Insufficient memory, 24 Too many pods",
		},
		{
			Time:    time.Now().Add(-1 * time.Minute),
			Type:    "Normal",
			Reason:  "Pulling",
			Message: "Pulling image",
		},
	}

	// Test that printEvents doesn't panic with FailedScheduling events
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("printEvents panicked: %v", r)
		}
	}()

	formatter.printEvents(events)
}

func TestWrapSchedulingMessage(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	tests := []struct {
		name     string
		message  string
		expected bool // true if message should be wrapped
	}{
		{
			name:     "Long FailedScheduling message should be wrapped",
			message:  "0/46 nodes are available: 1 Insufficient memory, 1 node(s) had untolerated taint, 18 node(s) didn't match Pod's node affinity/selector, 24 Too many pods. preemption: 0/46 nodes are available",
			expected: true,
		},
		{
			name:     "Short message should not be wrapped",
			message:  "Pod scheduled successfully",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.wrapSchedulingMessage(tt.message)

			// Check if the message was wrapped (contains newlines)
			isWrapped := strings.Contains(result, "\n")

			if tt.expected && !isWrapped {
				t.Errorf("Expected message to be wrapped but it wasn't")
			}
			if !tt.expected && isWrapped {
				t.Errorf("Expected message not to be wrapped but it was")
			}
		})
	}
}

func TestPodStatusDisplay(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	tests := []struct {
		name   string
		status string
	}{
		{"Pending pod status", "Pending"},
		{"Running pod status", "Running"},
		{"Failed pod status", "Failed"},
		{"Terminating pod status", "Terminating"},
		{"Succeeded pod status", "Succeeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := types.PodInfo{
				Name:     "test-pod",
				Status:   tt.status,
				NodeName: "test-node",
				Age:      time.Hour,
				Health:   types.HealthStatus{Level: "Healthy", Reason: "All containers running"},
			}

			// Test that printPodHeader doesn't panic with different statuses
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("printPodHeader panicked with status %s: %v", tt.status, r)
				}
			}()

			formatter.printPodHeader(pod)
		})
	}
}

func TestGetPodStatusColor(t *testing.T) {
	formatter := &Formatter{
		options: &types.Options{NoColor: false},
	}

	tests := []struct {
		status string
		name   string
	}{
		{"Running", "running status"},
		{"Pending", "pending status"},
		{"Failed", "failed status"},
		{"Terminating", "terminating status"},
		{"Succeeded", "succeeded status"},
		{"Unknown", "unknown status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color := formatter.getPodStatusColor(tt.status)
			// Just test that we get a color object
			if color == nil {
				t.Errorf("expected color object for status %s, got nil", tt.status)
			}
		})
	}
}
