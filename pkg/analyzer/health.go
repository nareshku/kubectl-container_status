package analyzer

import (
	"fmt"
	"strings"
	"time"

	"github.com/nareshku/kubectl-container-status/pkg/types"
)

// Analyzer handles health analysis and scoring
type Analyzer struct{}

// New creates a new analyzer instance
func New() *Analyzer {
	return &Analyzer{}
}

// AnalyzeWorkloadHealth analyzes the overall health of a workload
func (a *Analyzer) AnalyzeWorkloadHealth(workload types.WorkloadInfo) types.HealthStatus {
	if len(workload.Pods) == 0 {
		return types.HealthStatus{
			Level:  string(types.HealthLevelCritical),
			Reason: "no pods found",
			Score:  0,
		}
	}

	totalScore := 0
	criticalIssues := 0
	degradedIssues := 0

	for _, pod := range workload.Pods {
		podHealth := a.AnalyzePodHealth(pod)
		totalScore += podHealth.Score

		if podHealth.Level == string(types.HealthLevelCritical) {
			criticalIssues++
		} else if podHealth.Level == string(types.HealthLevelDegraded) {
			degradedIssues++
		}
	}

	averageScore := totalScore / len(workload.Pods)

	// Determine overall health level
	var level types.HealthLevel
	var reason string

	if criticalIssues > 0 {
		level = types.HealthLevelCritical
		if criticalIssues == 1 {
			reason = "1 pod has critical issues"
		} else {
			reason = fmt.Sprintf("%d pods have critical issues", criticalIssues)
		}
	} else if degradedIssues > 0 {
		level = types.HealthLevelDegraded
		if degradedIssues == 1 {
			reason = "1 pod has issues"
		} else {
			reason = fmt.Sprintf("%d pods have issues", degradedIssues)
		}
	} else {
		level = types.HealthLevelHealthy
		reason = "all pods running normally"
	}

	return types.HealthStatus{
		Level:  string(level),
		Reason: reason,
		Score:  averageScore,
	}
}

// AnalyzePodHealth analyzes the health of a single pod
func (a *Analyzer) AnalyzePodHealth(pod types.PodInfo) types.HealthStatus {
	score := 100 // Start with perfect score
	var issues []string

	// Check container statuses
	allContainers := append(pod.InitContainers, pod.Containers...)

	criticalContainers := 0
	degradedContainers := 0
	totalRestarts := int32(0)

	for _, container := range allContainers {
		containerHealth := a.analyzeContainerHealth(container)
		totalRestarts += container.RestartCount

		if containerHealth.Level == string(types.HealthLevelCritical) {
			criticalContainers++
			score -= 30
		} else if containerHealth.Level == string(types.HealthLevelDegraded) {
			degradedContainers++
			score -= 15
		}

		if containerHealth.Reason != "" {
			issues = append(issues, containerHealth.Reason)
		}
	}

	// Determine overall level
	var level types.HealthLevel
	var reason string

	if criticalContainers > 0 {
		level = types.HealthLevelCritical
		if len(issues) > 0 {
			reason = issues[0] // Take the first critical issue
		} else {
			reason = "containers in critical state"
		}
	} else if degradedContainers > 0 {
		level = types.HealthLevelDegraded
		if len(issues) > 0 {
			reason = issues[0] // Take the first degraded issue
		} else {
			reason = "containers have issues"
		}
	} else {
		level = types.HealthLevelHealthy
		reason = "all containers running normally"
	}

	// Ensure score doesn't go below 0
	if score < 0 {
		score = 0
	}

	return types.HealthStatus{
		Level:  string(level),
		Reason: reason,
		Score:  score,
	}
}

// analyzeContainerHealth analyzes the health of a single container
func (a *Analyzer) analyzeContainerHealth(container types.ContainerInfo) types.HealthStatus {
	score := 100
	var level types.HealthLevel
	var reason string

	// Check container status
	switch container.Status {
	case "CrashLoopBackOff":
		level = types.HealthLevelCritical
		reason = "container in CrashLoopBackOff"
		score = 0
	case "Error":
		level = types.HealthLevelCritical
		reason = "container in error state"
		score = 0
	case string(types.ContainerStatusTerminated):
		if container.Type != string(types.ContainerTypeInit) {
			level = types.HealthLevelCritical
			reason = "container terminated unexpectedly"
			score = 0
		} else {
			// Init containers should be terminated
			level = types.HealthLevelHealthy
		}
	case "ImagePullBackOff", "ErrImagePull":
		level = types.HealthLevelCritical
		reason = "cannot pull container image"
		score = 0
	case string(types.ContainerStatusWaiting):
		level = types.HealthLevelDegraded
		reason = "container waiting to start"
		score = 50
	case string(types.ContainerStatusRunning):
		level = types.HealthLevelHealthy
	case string(types.ContainerStatusCompleted):
		// Normal for init containers
		level = types.HealthLevelHealthy
	default:
		level = types.HealthLevelDegraded
		reason = "unknown container state"
		score = 30
	}

	// Only check exit codes if container is currently terminated (not just historical)
	if container.Status == string(types.ContainerStatusTerminated) && container.ExitCode != nil && *container.ExitCode != 0 {
		if level != types.HealthLevelCritical {
			level = types.HealthLevelDegraded
			reason = "terminated with non-zero exit code"
			score -= 20
		}
	}

	// Check restart count (only very recent restarts indicate current instability)
	if container.RestartCount > 0 {
		recentRestarts := a.hasRecentRestarts(container)
		if recentRestarts {
			if level == types.HealthLevelHealthy {
				level = types.HealthLevelDegraded
				reason = "recent restarts detected"
			}
			score -= 25 // Fixed penalty regardless of restart count
		}
	}

	// Check probes - liveness failures are critical, readiness failures are degraded
	if !container.Probes.Liveness.Passing && container.Probes.Liveness.Configured {
		level = types.HealthLevelCritical
		reason = "liveness probe failing"
		score = 0
	}

	if !container.Probes.Readiness.Passing && container.Probes.Readiness.Configured {
		if level == types.HealthLevelHealthy {
			level = types.HealthLevelDegraded
			reason = "readiness probe failing"
		}
		score -= 15
	}

	// Check resource usage - focus on actual constraints that affect performance
	if container.Resources.MemPercentage > 85 {
		if level == types.HealthLevelHealthy {
			level = types.HealthLevelDegraded
			reason = "high memory usage"
		}
		score -= 20
	}

	if container.Resources.CPUPercentage > 90 {
		if level == types.HealthLevelHealthy {
			level = types.HealthLevelDegraded
			reason = "high CPU usage"
		}
		score -= 15
	}

	// Check for OOMKilled
	if strings.Contains(container.TerminationReason, "OOMKilled") {
		level = types.HealthLevelCritical
		reason = "container killed due to out of memory"
		score = 0
	}

	// Ensure score doesn't go below 0
	if score < 0 {
		score = 0
	}

	return types.HealthStatus{
		Level:  string(level),
		Reason: reason,
		Score:  score,
	}
}

// hasRecentRestarts checks if container has had restarts in the last hour
func (a *Analyzer) hasRecentRestarts(container types.ContainerInfo) bool {
	// Check if there are restarts and the container was recently started
	// This is a conservative check - we consider restarts recent if the container
	// was started within the last hour, indicating possible recent restart activity
	if container.RestartCount == 0 {
		return false
	}

	if container.StartedAt == nil {
		return false
	}

	// Only consider restarts "recent" if the container started very recently
	// Focus on truly current instability, not historical issues
	return time.Since(*container.StartedAt) < 5*time.Minute
}

// GetHealthIcon returns the appropriate icon for health status
func (a *Analyzer) GetHealthIcon(level string) string {
	switch level {
	case string(types.HealthLevelHealthy):
		return "🟢" // Green circle - more visually appealing than plain checkmark
	case string(types.HealthLevelDegraded):
		return "🟡" // Yellow circle - stands out better than plain warning triangle
	case string(types.HealthLevelCritical):
		return "🔴" // Red circle - more prominent than plain X
	default:
		return "⚪" // White circle for unknown state
	}
}

// GetStatusIcon returns the appropriate icon for container status
func (a *Analyzer) GetStatusIcon(status string) string {
	switch status {
	case string(types.ContainerStatusRunning):
		return "🟢" // Green circle - consistent with health status
	case string(types.ContainerStatusCompleted):
		return "✅" // Check mark with green background - success indication
	case "CrashLoopBackOff", "Error":
		return "🔴" // Red circle - consistent critical status
	case string(types.ContainerStatusWaiting):
		return "🟡" // Yellow circle - waiting/warning state
	case string(types.ContainerStatusTerminated):
		return "🔴" // Red circle - terminated unexpectedly
	default:
		return "⚪" // White circle for unknown state
	}
}

// GetProbeIcon returns the appropriate icon for probe status
func (a *Analyzer) GetProbeIcon(passing bool, configured bool) string {
	if !configured {
		return ""
	}
	if passing {
		return "✅" // Check mark with green background - probe passing
	}
	return "❌" // Cross mark with red background - probe failing
}

// IsContainerProblematic checks if a container has issues
func (a *Analyzer) IsContainerProblematic(container types.ContainerInfo) bool {
	health := a.analyzeContainerHealth(container)
	return health.Level == string(types.HealthLevelCritical) ||
		health.Level == string(types.HealthLevelDegraded)
}
