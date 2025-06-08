package output

import (
	"testing"
	"time"

	"github.com/nareshku/kubectl-container-status/pkg/types"
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
