package collector

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	// Collect bulk metrics and events for better performance
	var bulkMetrics map[string]*types.PodMetrics
	var bulkEvents map[string][]types.EventInfo
	var err error

	// For workload views or when explicitly requested, collect bulk metrics
	if !options.SinglePodView || options.ShowResourceUsage {
		bulkMetrics, err = c.collectBulkMetrics(ctx, workload.Namespace, pods)
		if err != nil {
			fmt.Printf("Warning: Failed to collect bulk metrics: %v\n", err)
			bulkMetrics = make(map[string]*types.PodMetrics)
		}
	}

	// Collect bulk events when needed
	needsEvents := options.ShowEvents
	if needsEvents && len(pods) > 0 {
		bulkEvents, err = c.collectBulkEvents(ctx, workload.Namespace, pods, options)
		if err != nil {
			fmt.Printf("Warning: Failed to collect bulk events: %v\n", err)
			bulkEvents = make(map[string][]types.EventInfo)
		}
	}

	// Process pods in parallel for better performance
	type result struct {
		index int
		pod   *types.PodInfo
		err   error
	}

	results := make(chan result, len(pods))

	// Process each pod in a separate goroutine
	for i, pod := range pods {
		go func(index int, p corev1.Pod) {
			// Get pre-collected metrics and events for this pod
			var podMetrics *types.PodMetrics
			var podEvents []types.EventInfo

			if bulkMetrics != nil {
				podMetrics = bulkMetrics[p.Name]
			}
			if bulkEvents != nil {
				podEvents = bulkEvents[p.Name]
			}

			podInfo, err := c.collectPodInfoWithData(ctx, &p, options, podMetrics, podEvents)
			results <- result{index: index, pod: podInfo, err: err}
		}(i, pod)
	}

	// Collect results in order
	podInfos := make([]*types.PodInfo, len(pods))
	for i := 0; i < len(pods); i++ {
		res := <-results
		if res.err != nil {
			return nil, fmt.Errorf("failed to collect pod info for pod %d: %w", res.index, res.err)
		}
		podInfos[res.index] = res.pod
	}

	// Convert to slice of values
	var finalPods []types.PodInfo
	for _, podInfo := range podInfos {
		if podInfo != nil {
			finalPods = append(finalPods, *podInfo)
		}
	}

	return finalPods, nil
}

// collectPodInfo collects detailed information for a single pod
func (c *Collector) collectPodInfo(ctx context.Context, pod *corev1.Pod, options *types.Options) (*types.PodInfo, error) {
	// Determine pod status - check for terminating state first
	status := string(pod.Status.Phase)
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
	}

	podInfo := &types.PodInfo{
		Name:           pod.Name,
		Namespace:      pod.Namespace,
		NodeName:       pod.Spec.NodeName,
		ServiceAccount: pod.Spec.ServiceAccountName,
		Age:            time.Since(pod.CreationTimestamp.Time),
		Status:         status,
		Labels:         pod.Labels,
		Annotations:    pod.Annotations,
		Conditions:     c.collectPodConditions(pod),
		Network:        c.collectNetworkInfo(pod),
	}

	// Determine if this is a workload view (multiple pods) vs single pod view
	isWorkloadView := !options.SinglePodView

	// For workload view, only collect detailed data if specifically requested
	needsMetrics := !isWorkloadView || options.ShowResourceUsage
	needsEvents := options.ShowEvents || (!isWorkloadView && len(pod.OwnerReferences) == 0)
	needsDetailedInfo := !isWorkloadView || options.Wide || options.ShowEnv

	// Collect metrics only when needed
	var podMetrics *types.PodMetrics
	if needsMetrics && c.metricsClient != nil {
		metrics, err := c.collectPodMetrics(ctx, pod)
		if err != nil {
			// Metrics are optional, continue without them
			if !isWorkloadView {
				fmt.Printf("Warning: Failed to collect metrics for pod %s: %v\n", pod.Name, err)
			}
		}
		podMetrics = metrics
		podInfo.Metrics = metrics
	}

	// Collect container information - pass pod metrics for resource calculation
	for _, container := range pod.Spec.InitContainers {
		containerInfo := c.collectContainerInfo(ctx, container, pod, types.ContainerTypeInit, options, podMetrics, needsDetailedInfo)
		podInfo.InitContainers = append(podInfo.InitContainers, containerInfo)
	}

	for _, container := range pod.Spec.Containers {
		containerInfo := c.collectContainerInfo(ctx, container, pod, types.ContainerTypeStandard, options, podMetrics, needsDetailedInfo)
		podInfo.Containers = append(podInfo.Containers, containerInfo)
	}

	// Collect events only when needed
	if needsEvents {
		events, err := c.collectPodEvents(ctx, pod, options)
		if err != nil {
			// Events are optional, log warning but continue
			if !isWorkloadView {
				fmt.Printf("Warning: Failed to collect events for pod %s: %v\n", pod.Name, err)
			}
		}
		podInfo.Events = events
	}

	return podInfo, nil
}

// collectContainerInfo collects information for a single container
func (c *Collector) collectContainerInfo(ctx context.Context, container corev1.Container, pod *corev1.Pod, containerType types.ContainerType, options *types.Options, podMetrics *types.PodMetrics, needsDetailedInfo bool) types.ContainerInfo {
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

			// Set last restart time if container has been restarted
			if containerStatus.RestartCount > 0 {
				containerInfo.LastRestartTime = &containerStatus.State.Running.StartedAt.Time
			}
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

			// For terminated containers, if they had restarts, the last restart would be when they started
			if containerStatus.RestartCount > 0 {
				containerInfo.LastRestartTime = &containerStatus.State.Terminated.StartedAt.Time
			}
		}

		// Last state information and exit code from previous termination
		if containerStatus.LastTerminationState.Terminated != nil {
			containerInfo.LastState = "Terminated"
			containerInfo.LastStateReason = containerStatus.LastTerminationState.Terminated.Reason
			// Get exit code from last termination if current state doesn't have one
			if containerInfo.ExitCode == nil {
				containerInfo.ExitCode = &containerStatus.LastTerminationState.Terminated.ExitCode
			}
		} else if containerStatus.LastTerminationState.Waiting != nil {
			containerInfo.LastState = "Waiting"
			containerInfo.LastStateReason = containerStatus.LastTerminationState.Waiting.Reason
		} else {
			containerInfo.LastState = "None"
			containerInfo.LastStateReason = ""
		}
	} else {
		containerInfo.Status = string(types.ContainerStatusUnknown)
	}

	// Collect resource information with metrics
	containerInfo.Resources = c.collectResourceInfo(container, container.Name, podMetrics)

	// Collect probe information
	containerInfo.Probes = c.collectProbeInfo(container, containerStatus)

	// Collect volume information
	if needsDetailedInfo && options.Wide {
		containerInfo.Volumes = c.collectVolumeInfo(container, pod)
	}

	// Collect environment variables
	if needsDetailedInfo && options.ShowEnv {
		containerInfo.Environment = c.collectEnvironmentInfo(container, pod)
	}

	// Collect logs if requested (only for running containers to avoid errors)
	if options.ShowLogs && containerInfo.Status == string(types.ContainerStatusRunning) {
		logs, err := c.collectContainerLogs(ctx, pod, container.Name)
		if err != nil {
			// Logs are optional, continue without them but don't spam warnings
			// Only log error for single pod view
			if options.SinglePodView {
				fmt.Printf("Warning: Failed to collect logs for container %s: %v\n", container.Name, err)
			}
		} else {
			containerInfo.Logs = logs
		}
	}

	return containerInfo
}

// collectResourceInfo collects resource requests, limits, and usage
func (c *Collector) collectResourceInfo(container corev1.Container, containerName string, podMetrics *types.PodMetrics) types.ResourceInfo {
	resourceInfo := types.ResourceInfo{}

	if container.Resources.Requests != nil {
		if cpu := container.Resources.Requests.Cpu(); cpu != nil {
			resourceInfo.CPURequest = c.formatCPUUsage(cpu.String())
		}
		if memory := container.Resources.Requests.Memory(); memory != nil {
			resourceInfo.MemRequest = c.formatMemoryUsage(memory.String())
		}
	}

	if container.Resources.Limits != nil {
		if cpu := container.Resources.Limits.Cpu(); cpu != nil {
			resourceInfo.CPULimit = c.formatCPUUsage(cpu.String())
		}
		if memory := container.Resources.Limits.Memory(); memory != nil {
			resourceInfo.MemLimit = c.formatMemoryUsage(memory.String())
		}
	}

	// Initialize with default values
	resourceInfo.CPUUsage = "0m"
	resourceInfo.CPUPercentage = 0.0
	resourceInfo.MemUsage = "0Mi"
	resourceInfo.MemPercentage = 0.0

	// Use actual metrics if available
	if podMetrics != nil {
		containerMetrics := c.findContainerMetrics(podMetrics, containerName)
		if containerMetrics != nil {
			// Set CPU usage and calculate percentage
			if containerMetrics.CPUUsage != "" {
				resourceInfo.CPUUsage = c.formatCPUUsage(containerMetrics.CPUUsage)
				if resourceInfo.CPULimit != "" {
					resourceInfo.CPUPercentage = c.calculateCPUPercentage(containerMetrics.CPUUsage, resourceInfo.CPULimit)
				}
			}

			// Set memory usage and calculate percentage
			if containerMetrics.MemoryUsage != "" {
				resourceInfo.MemUsage = c.formatMemoryUsage(containerMetrics.MemoryUsage)
				if resourceInfo.MemLimit != "" {
					resourceInfo.MemPercentage = c.calculateMemoryPercentage(containerMetrics.MemoryUsage, resourceInfo.MemLimit)
				}
			}
		}
	}

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
func (c *Collector) collectEnvironmentInfo(container corev1.Container, pod *corev1.Pod) []types.EnvVar {
	var envVars []types.EnvVar

	for _, env := range container.Env {
		envVar := types.EnvVar{
			Name:   env.Name,
			Value:  env.Value,
			Masked: c.isSensitiveEnvVar(env.Name),
		}

		// Handle valueFrom references
		if env.Value == "" && env.ValueFrom != nil {
			if env.ValueFrom.FieldRef != nil {
				// Resolve field references
				switch env.ValueFrom.FieldRef.FieldPath {
				case "metadata.name":
					envVar.Value = pod.Name
				case "metadata.namespace":
					envVar.Value = pod.Namespace
				case "metadata.uid":
					envVar.Value = string(pod.UID)
				case "spec.nodeName":
					envVar.Value = pod.Spec.NodeName
				case "spec.serviceAccountName":
					envVar.Value = pod.Spec.ServiceAccountName
				case "status.hostIP":
					envVar.Value = pod.Status.HostIP
				case "status.podIP":
					envVar.Value = pod.Status.PodIP
				default:
					envVar.Value = fmt.Sprintf("[fieldRef:%s]", env.ValueFrom.FieldRef.FieldPath)
				}
			} else if env.ValueFrom.SecretKeyRef != nil {
				envVar.Value = fmt.Sprintf("[secret:%s/%s]", env.ValueFrom.SecretKeyRef.Name, env.ValueFrom.SecretKeyRef.Key)
				envVar.Masked = true // Secrets should be masked
			} else if env.ValueFrom.ConfigMapKeyRef != nil {
				envVar.Value = fmt.Sprintf("[configMap:%s/%s]", env.ValueFrom.ConfigMapKeyRef.Name, env.ValueFrom.ConfigMapKeyRef.Key)
			} else if env.ValueFrom.ResourceFieldRef != nil {
				envVar.Value = fmt.Sprintf("[resource:%s]", env.ValueFrom.ResourceFieldRef.Resource)
			} else {
				envVar.Value = "[valueFrom:unknown]"
			}
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
func (c *Collector) collectPodEvents(ctx context.Context, pod *corev1.Pod, options *types.Options) ([]types.EventInfo, error) {
	events, err := c.clientset.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + pod.Name,
	})
	if err != nil {
		return nil, err
	}

	var eventInfos []types.EventInfo

	// Default: last 5 minutes for automatic event display
	// With --events flag: last 1 hour for comprehensive view
	var cutoffTime time.Time
	if options.ShowEvents {
		cutoffTime = time.Now().Add(-1 * time.Hour) // Last 1 hour when explicitly requested
	} else {
		cutoffTime = time.Now().Add(-5 * time.Minute) // Last 5 minutes for brief view
	}

	for _, event := range events.Items {
		// Handle both old and new event formats
		var eventTime time.Time

		// For newer events, use EventTime or Series.LastObservedTime
		if !event.EventTime.IsZero() {
			eventTime = event.EventTime.Time
			// If there's a series with more recent observation, use that
			if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
				eventTime = event.Series.LastObservedTime.Time
			}
		} else {
			// Fallback to older format: use LastTimestamp if available, otherwise FirstTimestamp
			eventTime = event.FirstTimestamp.Time
			if !event.LastTimestamp.IsZero() {
				eventTime = event.LastTimestamp.Time
			}
		}

		if eventTime.After(cutoffTime) {
			eventInfo := types.EventInfo{
				Time:    eventTime,
				Type:    event.Type,
				Reason:  event.Reason,
				Message: event.Message,
				PodName: pod.Name,
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

	metrics := &types.PodMetrics{
		Containers: make(map[string]types.ContainerMetrics),
	}

	// Store metrics for each container
	for _, container := range podMetrics.Containers {
		containerMetrics := types.ContainerMetrics{}
		if cpu := container.Usage.Cpu(); cpu != nil {
			containerMetrics.CPUUsage = cpu.String()
		}
		if memory := container.Usage.Memory(); memory != nil {
			containerMetrics.MemoryUsage = memory.String()
		}
		metrics.Containers[container.Name] = containerMetrics
	}

	return metrics, nil
}

// findContainerMetrics finds metrics for a specific container in pod metrics
func (c *Collector) findContainerMetrics(podMetrics *types.PodMetrics, containerName string) *types.ContainerMetrics {
	if podMetrics == nil || podMetrics.Containers == nil {
		return nil
	}

	if metrics, exists := podMetrics.Containers[containerName]; exists {
		return &metrics
	}

	return nil
}

// calculateCPUPercentage calculates CPU usage percentage
func (c *Collector) calculateCPUPercentage(usage, limit string) float64 {
	usageQuantity, err := resource.ParseQuantity(usage)
	if err != nil {
		return 0.0
	}

	limitQuantity, err := resource.ParseQuantity(limit)
	if err != nil {
		return 0.0
	}

	if limitQuantity.IsZero() {
		return 0.0
	}

	// Convert to milliCPUs for calculation
	usageMilliCPU := usageQuantity.MilliValue()
	limitMilliCPU := limitQuantity.MilliValue()

	percentage := float64(usageMilliCPU) / float64(limitMilliCPU) * 100
	return percentage
}

// calculateMemoryPercentage calculates memory usage percentage
func (c *Collector) calculateMemoryPercentage(usage, limit string) float64 {
	usageQuantity, err := resource.ParseQuantity(usage)
	if err != nil {
		return 0.0
	}

	limitQuantity, err := resource.ParseQuantity(limit)
	if err != nil {
		return 0.0
	}

	if limitQuantity.IsZero() {
		return 0.0
	}

	// Convert to bytes for calculation
	usageBytes := usageQuantity.Value()
	limitBytes := limitQuantity.Value()

	percentage := float64(usageBytes) / float64(limitBytes) * 100
	return percentage
}

// formatCPUUsage formats CPU usage to human-readable format (like kubectl top)
func (c *Collector) formatCPUUsage(usage string) string {
	quantity, err := resource.ParseQuantity(usage)
	if err != nil {
		return usage
	}

	// Convert to millicores
	milliCPU := quantity.MilliValue()

	if milliCPU >= 1000 {
		// Show as cores if >= 1 core
		cores := float64(milliCPU) / 1000.0
		if cores >= 10 {
			return fmt.Sprintf("%.0f", cores)
		}
		return fmt.Sprintf("%.1f", cores)
	}

	// Show as millicores
	return fmt.Sprintf("%dm", milliCPU)
}

// formatMemoryUsage formats memory usage to human-readable format (like kubectl top)
func (c *Collector) formatMemoryUsage(usage string) string {
	quantity, err := resource.ParseQuantity(usage)
	if err != nil {
		return usage
	}

	// Get bytes value
	bytes := quantity.Value()

	// Convert to appropriate unit
	const (
		Ki = 1024
		Mi = Ki * 1024
		Gi = Mi * 1024
		Ti = Gi * 1024
	)

	if bytes >= Ti {
		return fmt.Sprintf("%.1fTi", float64(bytes)/Ti)
	} else if bytes >= Gi {
		return fmt.Sprintf("%.1fGi", float64(bytes)/Gi)
	} else if bytes >= Mi {
		return fmt.Sprintf("%dMi", bytes/Mi)
	} else if bytes >= Ki {
		return fmt.Sprintf("%dKi", bytes/Ki)
	}

	return fmt.Sprintf("%d", bytes)
}

// collectBulkMetrics collects metrics for all pods in one API call
func (c *Collector) collectBulkMetrics(ctx context.Context, namespace string, pods []corev1.Pod) (map[string]*types.PodMetrics, error) {
	if c.metricsClient == nil {
		return nil, fmt.Errorf("metrics client not available")
	}

	// Get all pod metrics in the namespace
	podMetricsList, err := c.metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Create a map for fast lookup
	result := make(map[string]*types.PodMetrics)

	// Convert to our format and index by pod name
	for _, podMetrics := range podMetricsList.Items {
		metrics := &types.PodMetrics{
			Containers: make(map[string]types.ContainerMetrics),
		}

		// Store metrics for each container
		for _, container := range podMetrics.Containers {
			containerMetrics := types.ContainerMetrics{}
			if cpu := container.Usage.Cpu(); cpu != nil {
				containerMetrics.CPUUsage = cpu.String()
			}
			if memory := container.Usage.Memory(); memory != nil {
				containerMetrics.MemoryUsage = memory.String()
			}
			metrics.Containers[container.Name] = containerMetrics
		}

		result[podMetrics.Name] = metrics
	}

	return result, nil
}

// collectBulkEvents collects events for all pods in one API call
func (c *Collector) collectBulkEvents(ctx context.Context, namespace string, pods []corev1.Pod, options *types.Options) (map[string][]types.EventInfo, error) {
	// Get all events in the namespace
	events, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Create a map of pod names for fast lookup
	podNames := make(map[string]bool)
	for _, pod := range pods {
		podNames[pod.Name] = true
	}

	// Determine time cutoff
	var cutoffTime time.Time
	if options.ShowEvents {
		cutoffTime = time.Now().Add(-1 * time.Hour) // Last 1 hour when explicitly requested
	} else {
		cutoffTime = time.Now().Add(-5 * time.Minute) // Last 5 minutes for brief view
	}

	// Group events by pod name
	result := make(map[string][]types.EventInfo)

	for _, event := range events.Items {
		// Check if this event is for one of our pods
		if !podNames[event.InvolvedObject.Name] {
			continue
		}

		// Handle both old and new event formats
		var eventTime time.Time

		// For newer events, use EventTime or Series.LastObservedTime
		if !event.EventTime.IsZero() {
			eventTime = event.EventTime.Time
			// If there's a series with more recent observation, use that
			if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
				eventTime = event.Series.LastObservedTime.Time
			}
		} else {
			// Fallback to older format: use LastTimestamp if available, otherwise FirstTimestamp
			eventTime = event.FirstTimestamp.Time
			if !event.LastTimestamp.IsZero() {
				eventTime = event.LastTimestamp.Time
			}
		}

		if eventTime.After(cutoffTime) {
			podName := event.InvolvedObject.Name
			eventInfo := types.EventInfo{
				Time:    eventTime,
				Type:    event.Type,
				Reason:  event.Reason,
				Message: event.Message,
				PodName: podName,
			}

			result[podName] = append(result[podName], eventInfo)
		}
	}

	return result, nil
}

// collectPodInfoWithData collects pod information using pre-collected metrics and events
func (c *Collector) collectPodInfoWithData(ctx context.Context, pod *corev1.Pod, options *types.Options, podMetrics *types.PodMetrics, podEvents []types.EventInfo) (*types.PodInfo, error) {
	// Determine pod status - check for terminating state first
	status := string(pod.Status.Phase)
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
	}

	podInfo := &types.PodInfo{
		Name:           pod.Name,
		Namespace:      pod.Namespace,
		NodeName:       pod.Spec.NodeName,
		ServiceAccount: pod.Spec.ServiceAccountName,
		Age:            time.Since(pod.CreationTimestamp.Time),
		Status:         status,
		Metrics:        podMetrics,
		Events:         podEvents,
		Labels:         pod.Labels,
		Annotations:    pod.Annotations,
		Conditions:     c.collectPodConditions(pod),
		Network:        c.collectNetworkInfo(pod),
	}

	// Determine if detailed info is needed
	needsDetailedInfo := options.SinglePodView || options.Wide || options.ShowEnv

	// Collect container information - pass pod metrics for resource calculation
	for _, container := range pod.Spec.InitContainers {
		containerInfo := c.collectContainerInfo(ctx, container, pod, types.ContainerTypeInit, options, podMetrics, needsDetailedInfo)
		podInfo.InitContainers = append(podInfo.InitContainers, containerInfo)
	}

	for _, container := range pod.Spec.Containers {
		containerInfo := c.collectContainerInfo(ctx, container, pod, types.ContainerTypeStandard, options, podMetrics, needsDetailedInfo)
		podInfo.Containers = append(podInfo.Containers, containerInfo)
	}

	return podInfo, nil
}

// collectContainerLogs collects recent logs for a container
func (c *Collector) collectContainerLogs(ctx context.Context, pod *corev1.Pod, containerName string) ([]string, error) {
	// Just get the most recent 10 lines, like systemctl status
	logOptions := &corev1.PodLogOptions{
		Container:  containerName,
		Follow:     false,
		Timestamps: false,
		TailLines:  int64Ptr(10), // Last 10 lines, no time filtering
	}

	// Get logs
	req := c.clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions)
	logs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	// Read logs
	var logLines []string
	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			logLines = append(logLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read logs: %w", err)
	}

	return logLines, nil
}

// collectPodConditions collects pod condition information
func (c *Collector) collectPodConditions(pod *corev1.Pod) []types.PodCondition {
	var conditions []types.PodCondition

	for _, condition := range pod.Status.Conditions {
		podCondition := types.PodCondition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}
		conditions = append(conditions, podCondition)
	}

	return conditions
}

// int64Ptr returns a pointer to an int64 value
func int64Ptr(i int64) *int64 {
	return &i
}

// collectNetworkInfo collects network information for a pod
func (c *Collector) collectNetworkInfo(pod *corev1.Pod) types.NetworkInfo {
	networkInfo := types.NetworkInfo{
		HostNetwork: pod.Spec.HostNetwork,
		PodIP:       pod.Status.PodIP,
		HostIP:      pod.Status.HostIP,
	}

	// Collect all pod IPs for dual-stack support
	if len(pod.Status.PodIPs) > 0 {
		for _, podIP := range pod.Status.PodIPs {
			networkInfo.PodIPs = append(networkInfo.PodIPs, podIP.IP)
		}
	} else if pod.Status.PodIP != "" {
		// Fallback to single PodIP if PodIPs is not available
		networkInfo.PodIPs = append(networkInfo.PodIPs, pod.Status.PodIP)
	}

	return networkInfo
}
