package analyzer

import (
	"testing"
	"time"

	"github.com/nareshku/kubectl-container-status/pkg/types"
)

func TestAnalyzeContainerHealth(t *testing.T) {
	analyzer := New()

	tests := []struct {
		name      string
		container types.ContainerInfo
		expected  types.HealthStatus
	}{
		{
			name: "healthy running container",
			container: types.ContainerInfo{
				Name:         "test-container",
				Type:         string(types.ContainerTypeStandard),
				Status:       string(types.ContainerStatusRunning),
				Ready:        true,
				RestartCount: 0,
				Probes: types.ProbeInfo{
					Liveness:  types.ProbeDetails{Configured: true, Passing: true},
					Readiness: types.ProbeDetails{Configured: true, Passing: true},
				},
				Resources: types.ResourceInfo{
					MemPercentage: 50.0,
					CPUPercentage: 30.0,
				},
			},
			expected: types.HealthStatus{
				Level:  string(types.HealthLevelHealthy),
				Reason: "",
				Score:  100,
			},
		},
		{
			name: "container in CrashLoopBackOff",
			container: types.ContainerInfo{
				Name:         "failing-container",
				Type:         string(types.ContainerTypeStandard),
				Status:       "CrashLoopBackOff",
				Ready:        false,
				RestartCount: 5,
			},
			expected: types.HealthStatus{
				Level:  string(types.HealthLevelCritical),
				Reason: "container in CrashLoopBackOff",
				Score:  0,
			},
		},
		{
			name: "container with high memory usage",
			container: types.ContainerInfo{
				Name:         "memory-hungry",
				Type:         string(types.ContainerTypeStandard),
				Status:       string(types.ContainerStatusRunning),
				Ready:        true,
				RestartCount: 0,
				Resources: types.ResourceInfo{
					MemPercentage: 95.0,
					CPUPercentage: 30.0,
				},
			},
			expected: types.HealthStatus{
				Level:  string(types.HealthLevelDegraded),
				Reason: "high memory usage",
				Score:  80,
			},
		},
		{
			name: "container with failing readiness probe",
			container: types.ContainerInfo{
				Name:         "not-ready",
				Type:         string(types.ContainerTypeStandard),
				Status:       string(types.ContainerStatusRunning),
				Ready:        false,
				RestartCount: 0,
				Probes: types.ProbeInfo{
					Liveness:  types.ProbeDetails{Configured: true, Passing: true},
					Readiness: types.ProbeDetails{Configured: true, Passing: false},
				},
				Resources: types.ResourceInfo{
					MemPercentage: 50.0,
					CPUPercentage: 30.0,
				},
			},
			expected: types.HealthStatus{
				Level:  string(types.HealthLevelDegraded),
				Reason: "readiness probe failing",
				Score:  85,
			},
		},
		{
			name: "init container completed successfully",
			container: types.ContainerInfo{
				Name:     "init-container",
				Type:     string(types.ContainerTypeInit),
				Status:   string(types.ContainerStatusCompleted),
				Ready:    false,
				ExitCode: func() *int32 { code := int32(0); return &code }(),
			},
			expected: types.HealthStatus{
				Level:  string(types.HealthLevelHealthy),
				Reason: "",
				Score:  100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.analyzeContainerHealth(tt.container)

			if result.Level != tt.expected.Level {
				t.Errorf("expected level %s, got %s", tt.expected.Level, result.Level)
			}

			if tt.expected.Reason != "" && result.Reason != tt.expected.Reason {
				t.Errorf("expected reason '%s', got '%s'", tt.expected.Reason, result.Reason)
			}

			if result.Score != tt.expected.Score {
				t.Errorf("expected score %d, got %d", tt.expected.Score, result.Score)
			}
		})
	}
}

func TestAnalyzePodHealth(t *testing.T) {
	analyzer := New()

	tests := []struct {
		name     string
		pod      types.PodInfo
		expected types.HealthLevel
	}{
		{
			name: "healthy pod with running containers",
			pod: types.PodInfo{
				Name: "healthy-pod",
				Containers: []types.ContainerInfo{
					{
						Name:         "container1",
						Type:         string(types.ContainerTypeStandard),
						Status:       string(types.ContainerStatusRunning),
						Ready:        true,
						RestartCount: 0,
					},
					{
						Name:         "container2",
						Type:         string(types.ContainerTypeStandard),
						Status:       string(types.ContainerStatusRunning),
						Ready:        true,
						RestartCount: 0,
					},
				},
			},
			expected: types.HealthLevelHealthy,
		},
		{
			name: "pod with one critical container",
			pod: types.PodInfo{
				Name: "problematic-pod",
				Containers: []types.ContainerInfo{
					{
						Name:         "good-container",
						Type:         string(types.ContainerTypeStandard),
						Status:       string(types.ContainerStatusRunning),
						Ready:        true,
						RestartCount: 0,
					},
					{
						Name:         "bad-container",
						Type:         string(types.ContainerTypeStandard),
						Status:       "CrashLoopBackOff",
						Ready:        false,
						RestartCount: 3,
					},
				},
			},
			expected: types.HealthLevelCritical,
		},
		{
			name: "pod with degraded container",
			pod: types.PodInfo{
				Name: "degraded-pod",
				Containers: []types.ContainerInfo{
					{
						Name:         "container",
						Type:         string(types.ContainerTypeStandard),
						Status:       string(types.ContainerStatusRunning),
						Ready:        false,
						RestartCount: 1,
						StartedAt:    func() *time.Time { t := time.Now().Add(-3 * time.Minute); return &t }(),
						Resources: types.ResourceInfo{
							MemPercentage: 85.0,
						},
					},
				},
			},
			expected: types.HealthLevelDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.AnalyzePodHealth(tt.pod)

			if result.Level != string(tt.expected) {
				t.Errorf("expected level %s, got %s", tt.expected, result.Level)
			}
		})
	}
}

func TestAnalyzeWorkloadHealth(t *testing.T) {
	analyzer := New()

	tests := []struct {
		name     string
		workload types.WorkloadInfo
		expected types.HealthLevel
	}{
		{
			name: "healthy workload",
			workload: types.WorkloadInfo{
				Name: "healthy-deployment",
				Kind: "Deployment",
				Pods: []types.PodInfo{
					{
						Name: "pod1",
						Health: types.HealthStatus{
							Level: string(types.HealthLevelHealthy),
							Score: 100,
						},
					},
					{
						Name: "pod2",
						Health: types.HealthStatus{
							Level: string(types.HealthLevelHealthy),
							Score: 100,
						},
					},
				},
			},
			expected: types.HealthLevelHealthy,
		},
		{
			name: "workload with critical pod",
			workload: types.WorkloadInfo{
				Name: "problematic-deployment",
				Kind: "Deployment",
				Pods: []types.PodInfo{
					{
						Name: "healthy-pod",
						Containers: []types.ContainerInfo{
							{
								Name:   "good-container",
								Status: string(types.ContainerStatusRunning),
								Ready:  true,
							},
						},
						Health: types.HealthStatus{
							Level: string(types.HealthLevelHealthy),
							Score: 100,
						},
					},
					{
						Name: "critical-pod",
						Containers: []types.ContainerInfo{
							{
								Name:   "bad-container",
								Status: "CrashLoopBackOff",
								Ready:  false,
							},
						},
						Health: types.HealthStatus{
							Level: string(types.HealthLevelCritical),
							Score: 0,
						},
					},
				},
			},
			expected: types.HealthLevelCritical,
		},
		{
			name: "empty workload",
			workload: types.WorkloadInfo{
				Name: "empty-deployment",
				Kind: "Deployment",
				Pods: []types.PodInfo{},
			},
			expected: types.HealthLevelCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.AnalyzeWorkloadHealth(tt.workload)

			if result.Level != string(tt.expected) {
				t.Errorf("expected level %s, got %s", tt.expected, result.Level)
			}
		})
	}
}

func TestGetHealthIcon(t *testing.T) {
	analyzer := New()

	tests := []struct {
		level    string
		expected string
	}{
		{string(types.HealthLevelHealthy), "🟢"},
		{string(types.HealthLevelDegraded), "🟡"},
		{string(types.HealthLevelCritical), "🔴"},
		{"unknown", "⚪"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			result := analyzer.GetHealthIcon(tt.level)
			if result != tt.expected {
				t.Errorf("expected icon %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetStatusIcon(t *testing.T) {
	analyzer := New()

	tests := []struct {
		status   string
		expected string
	}{
		{string(types.ContainerStatusRunning), "🟢"},
		{string(types.ContainerStatusCompleted), "✅"},
		{"CrashLoopBackOff", "🔴"},
		{"Error", "🔴"},
		{string(types.ContainerStatusWaiting), "🟡"},
		{string(types.ContainerStatusTerminated), "🔴"},
		{"unknown", "⚪"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := analyzer.GetStatusIcon(tt.status)
			if result != tt.expected {
				t.Errorf("expected icon %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestIsContainerProblematic(t *testing.T) {
	analyzer := New()

	tests := []struct {
		name      string
		container types.ContainerInfo
		expected  bool
	}{
		{
			name: "healthy container",
			container: types.ContainerInfo{
				Status:       string(types.ContainerStatusRunning),
				RestartCount: 0,
				Resources:    types.ResourceInfo{MemPercentage: 50.0},
				Probes: types.ProbeInfo{
					Liveness:  types.ProbeDetails{Configured: true, Passing: true},
					Readiness: types.ProbeDetails{Configured: true, Passing: true},
				},
			},
			expected: false,
		},
		{
			name: "container with restarts",
			container: types.ContainerInfo{
				Status:       string(types.ContainerStatusRunning),
				RestartCount: 3,
				StartedAt:    func() *time.Time { t := time.Now().Add(-3 * time.Minute); return &t }(),
				Resources:    types.ResourceInfo{MemPercentage: 50.0},
			},
			expected: true,
		},
		{
			name: "container in crash loop",
			container: types.ContainerInfo{
				Status:       "CrashLoopBackOff",
				RestartCount: 0,
			},
			expected: true,
		},
		{
			name: "container with high memory usage",
			container: types.ContainerInfo{
				Status:       string(types.ContainerStatusRunning),
				RestartCount: 0,
				Resources:    types.ResourceInfo{MemPercentage: 95.0},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.IsContainerProblematic(tt.container)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
