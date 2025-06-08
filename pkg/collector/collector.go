package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/nareshku/kubectl-container-status/pkg/types"
)

// Collector handles data collection from Kubernetes API
type Collector struct {
	clientset     kubernetes.Interface
	metricsClient metricsv1beta1.Interface
}

// New creates a new collector instance
func New(clientset kubernetes.Interface, metricsClient metricsv1beta1.Interface) *Collector {
	return &Collector{
		clientset:     clientset,
		metricsClient: metricsClient,
	}
}

// CollectPods collects pod information for a workload
func (c *Collector) CollectPods(ctx context.Context, workload types.WorkloadInfo, options *types.Options) ([]types.PodInfo, error) {
	var pods []corev1.Pod

	if workload.Kind == "Pod" {
		// Single pod
		pod, err := c.clientset.CoreV1().Pods(workload.Namespace).Get(ctx, workload.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get pod: %w", err)
		}
		pods = append(pods, *pod)
	} else {
		// Workload with selector
		selector := labels.SelectorFromSet(workload.Selector)
		podList, err := c.clientset.CoreV1().Pods(workload.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector.String(),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}
		pods = podList.Items
	}

	var podInfos []types.PodInfo
	for _, pod := range pods {
		podInfo, err := c.collectPodInfo(ctx, &pod, options)
		if err != nil {
			return nil, fmt.Errorf("failed to collect pod info for %s: %w", pod.Name, err)
		}
		podInfos = append(podInfos, *podInfo)
	}

	return podInfos, nil
}

// collectPodInfo collects detailed information for a single pod
func (c *Collector) collectPodInfo(ctx context.Context, pod *corev1.Pod, options *types.Options) (*types.PodInfo, error) {
	// Determine pod status - check for terminating state first
	status := string(pod.Status.Phase)
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
	}

	podInfo := &types.PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		NodeName:  pod.Spec.NodeName,
		Age:       time.Since(pod.CreationTimestamp.Time),
		Status:    status,
	}

	// Collect container information
	for _, container := range pod.Spec.InitContainers {
		containerInfo := c.collectContainerInfo(container, pod, types.ContainerTypeInit, options)
		podInfo.InitContainers = append(podInfo.InitContainers, containerInfo)
	}

	for _, container := range pod.Spec.Containers {
		containerInfo := c.collectContainerInfo(container, pod, types.ContainerTypeStandard, options)
		podInfo.Containers = append(podInfo.Containers, containerInfo)
	}

	// Collect events
	events, err := c.collectPodEvents(ctx, pod)
	if err != nil {
		// Events are optional, log warning but continue
		fmt.Printf("Warning: Failed to collect events for pod %s: %v\n", pod.Name, err)
	}
	podInfo.Events = events

	// Collect metrics if available
	if c.metricsClient != nil {
		metrics, err := c.collectPodMetrics(ctx, pod)
		if err != nil {
			// Metrics are optional, continue without them
			fmt.Printf("Warning: Failed to collect metrics for pod %s: %v\n", pod.Name, err)
		}
		podInfo.Metrics = metrics
	}

	return podInfo, nil
}

// collectContainerInfo collects information for a single container
func (c *Collector) collectContainerInfo(container corev1.Container, pod *corev1.Pod, containerType types.ContainerType, options *types.Options) types.ContainerInfo {
	containerInfo := types.ContainerInfo{
		Name:    container.Name,
		Type:    string(containerType),
		Image:   container.Image,
		Command: container.Command,
		Args:    container.Args,
	}

	// Find container status
	var containerStatus *corev1.ContainerStatus
	if containerType == types.ContainerTypeInit {
		for i, status := range pod.Status.InitContainerStatuses {
			if status.Name == container.Name {
				containerStatus = &pod.Status.InitContainerStatuses[i]
				break
			}
		}
	} else {
		for i, status := range pod.Status.ContainerStatuses {
			if status.Name == container.Name {
				containerStatus = &pod.Status.ContainerStatuses[i]
				break
			}
		}
	}

	if containerStatus != nil {
		containerInfo.Ready = containerStatus.Ready
		containerInfo.RestartCount = containerStatus.RestartCount

		// Determine status and details
		if containerStatus.State.Running != nil {
			containerInfo.Status = string(types.ContainerStatusRunning)
			containerInfo.StartedAt = &containerStatus.State.Running.StartedAt.Time
		} else if containerStatus.State.Waiting != nil {
			containerInfo.Status = containerStatus.State.Waiting.Reason
			if containerInfo.Status == "" {
				containerInfo.Status = string(types.ContainerStatusWaiting)
			}
		} else if containerStatus.State.Terminated != nil {
			if containerType == types.ContainerTypeInit && containerStatus.State.Terminated.ExitCode == 0 {
				containerInfo.Status = string(types.ContainerStatusCompleted)
			} else {
				containerInfo.Status = string(types.ContainerStatusTerminated)
			}
			containerInfo.ExitCode = &containerStatus.State.Terminated.ExitCode
			containerInfo.StartedAt = &containerStatus.State.Terminated.StartedAt.Time
			containerInfo.FinishedAt = &containerStatus.State.Terminated.FinishedAt.Time
			containerInfo.TerminationReason = containerStatus.State.Terminated.Reason
		}

		// Last state information and exit code from previous termination
		if containerStatus.LastTerminationState.Terminated != nil {
			containerInfo.LastState = "Terminated"
			// Get exit code from last termination if current state doesn't have one
			if containerInfo.ExitCode == nil {
				containerInfo.ExitCode = &containerStatus.LastTerminationState.Terminated.ExitCode
			}
		} else if containerStatus.LastTerminationState.Waiting != nil {
			containerInfo.LastState = "Waiting"
		} else {
			containerInfo.LastState = "None"
		}
	} else {
		containerInfo.Status = string(types.ContainerStatusUnknown)
	}

	// Collect resource information
	containerInfo.Resources = c.collectResourceInfo(container)

	// Collect probe information
	containerInfo.Probes = c.collectProbeInfo(container, containerStatus)

	// Collect volume information
	if options.Wide {
		containerInfo.Volumes = c.collectVolumeInfo(container, pod)
	}

	// Collect environment variables
	if options.ShowEnv {
		containerInfo.Environment = c.collectEnvironmentInfo(container)
	}

	return containerInfo
}

// collectResourceInfo collects resource requests, limits, and usage
func (c *Collector) collectResourceInfo(container corev1.Container) types.ResourceInfo {
	resourceInfo := types.ResourceInfo{}

	if container.Resources.Requests != nil {
		if cpu := container.Resources.Requests.Cpu(); cpu != nil {
			resourceInfo.CPURequest = cpu.String()
		}
		if memory := container.Resources.Requests.Memory(); memory != nil {
			resourceInfo.MemRequest = memory.String()
		}
	}

	if container.Resources.Limits != nil {
		if cpu := container.Resources.Limits.Cpu(); cpu != nil {
			resourceInfo.CPULimit = cpu.String()
		}
		if memory := container.Resources.Limits.Memory(); memory != nil {
			resourceInfo.MemLimit = memory.String()
		}
	}

	// Usage information would come from metrics server
	// This is a placeholder - actual implementation would use metrics client
	resourceInfo.CPUUsage = "0m"
	resourceInfo.CPUPercentage = 0.0
	resourceInfo.MemUsage = "0Mi"
	resourceInfo.MemPercentage = 0.0

	return resourceInfo
}

// collectProbeInfo collects probe configuration and status
func (c *Collector) collectProbeInfo(container corev1.Container, status *corev1.ContainerStatus) types.ProbeInfo {
	probeInfo := types.ProbeInfo{}

	// Liveness probe
	if container.LivenessProbe != nil {
		probeInfo.Liveness = c.parseProbeDetails(container.LivenessProbe)
		probeInfo.Liveness.Configured = true
		// In a real implementation, we'd check the actual probe status
		probeInfo.Liveness.Passing = true // Default assumption
	}

	// Readiness probe
	if container.ReadinessProbe != nil {
		probeInfo.Readiness = c.parseProbeDetails(container.ReadinessProbe)
		probeInfo.Readiness.Configured = true
		if status != nil {
			probeInfo.Readiness.Passing = status.Ready
		}
	}

	// Startup probe
	if container.StartupProbe != nil {
		probeInfo.Startup = c.parseProbeDetails(container.StartupProbe)
		probeInfo.Startup.Configured = true
		probeInfo.Startup.Passing = true // Default assumption
	}

	return probeInfo
}

// parseProbeDetails parses probe configuration details
func (c *Collector) parseProbeDetails(probe *corev1.Probe) types.ProbeDetails {
	details := types.ProbeDetails{}

	if probe.HTTPGet != nil {
		details.Type = "HTTP"
		details.Path = probe.HTTPGet.Path
		details.Port = probe.HTTPGet.Port.String()
	} else if probe.TCPSocket != nil {
		details.Type = "TCP"
		details.Port = probe.TCPSocket.Port.String()
	} else if probe.Exec != nil {
		details.Type = "Exec"
	}

	return details
}

// collectVolumeInfo collects volume mount information
func (c *Collector) collectVolumeInfo(container corev1.Container, pod *corev1.Pod) []types.VolumeInfo {
	var volumes []types.VolumeInfo

	for _, mount := range container.VolumeMounts {
		volumeInfo := types.VolumeInfo{
			Name:      mount.Name,
			MountPath: mount.MountPath,
		}

		// Find the volume in pod spec to get more details
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == mount.Name {
				if volume.ConfigMap != nil {
					volumeInfo.VolumeType = "ConfigMap"
					volumeInfo.Details = fmt.Sprintf("configmap/%s", volume.ConfigMap.Name)
				} else if volume.Secret != nil {
					volumeInfo.VolumeType = "Secret"
					volumeInfo.Details = fmt.Sprintf("secret/%s", volume.Secret.SecretName)
				} else if volume.PersistentVolumeClaim != nil {
					volumeInfo.VolumeType = "PVC"
					volumeInfo.Details = fmt.Sprintf("pvc/%s", volume.PersistentVolumeClaim.ClaimName)
				} else if volume.EmptyDir != nil {
					volumeInfo.VolumeType = "EmptyDir"
					volumeInfo.Details = "emptyDir"
				} else {
					volumeInfo.VolumeType = "Other"
					volumeInfo.Details = "unknown"
				}
				break
			}
		}

		volumes = append(volumes, volumeInfo)
	}

	return volumes
}

// collectEnvironmentInfo collects environment variable information
func (c *Collector) collectEnvironmentInfo(container corev1.Container) []types.EnvVar {
	var envVars []types.EnvVar

	for _, env := range container.Env {
		envVar := types.EnvVar{
			Name:   env.Name,
			Value:  env.Value,
			Masked: c.isSensitiveEnvVar(env.Name),
		}

		if envVar.Masked {
			envVar.Value = "***"
		}

		envVars = append(envVars, envVar)
	}

	return envVars
}

// isSensitiveEnvVar checks if an environment variable should be masked
func (c *Collector) isSensitiveEnvVar(name string) bool {
	sensitivePatterns := []string{"PASSWORD", "SECRET", "KEY", "TOKEN", "PASS"}
	upperName := strings.ToUpper(name)

	for _, pattern := range sensitivePatterns {
		if strings.Contains(upperName, pattern) {
			return true
		}
	}
	return false
}

// collectPodEvents collects recent events for a pod
func (c *Collector) collectPodEvents(ctx context.Context, pod *corev1.Pod) ([]types.EventInfo, error) {
	events, err := c.clientset.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + pod.Name,
	})
	if err != nil {
		return nil, err
	}

	var eventInfos []types.EventInfo
	cutoffTime := time.Now().Add(-5 * time.Minute) // Last 5 minutes

	for _, event := range events.Items {
		if event.FirstTimestamp.Time.After(cutoffTime) {
			eventInfo := types.EventInfo{
				Time:    event.FirstTimestamp.Time,
				Type:    event.Type,
				Reason:  event.Reason,
				Message: event.Message,
			}
			eventInfos = append(eventInfos, eventInfo)
		}
	}

	return eventInfos, nil
}

// collectPodMetrics collects resource usage metrics for a pod
func (c *Collector) collectPodMetrics(ctx context.Context, pod *corev1.Pod) (*types.PodMetrics, error) {
	if c.metricsClient == nil {
		return nil, fmt.Errorf("metrics client not available")
	}

	podMetrics, err := c.metricsClient.MetricsV1beta1().PodMetricses(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	metrics := &types.PodMetrics{}

	for _, container := range podMetrics.Containers {
		if cpu := container.Usage.Cpu(); cpu != nil {
			metrics.CPUUsage = cpu.String()
		}
		if memory := container.Usage.Memory(); memory != nil {
			metrics.MemoryUsage = memory.String()
		}
		// For simplicity, just use the first container's metrics
		break
	}

	return metrics, nil
}
