package output

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"gopkg.in/yaml.v3"

	"github.com/nareshku/kubectl-container-status/pkg/analyzer"
	"github.com/nareshku/kubectl-container-status/pkg/types"
)

// Formatter handles output formatting
type Formatter struct {
	options  *types.Options
	analyzer *analyzer.Analyzer
}

// New creates a new formatter instance
func New(options *types.Options) *Formatter {
	return &Formatter{
		options:  options,
		analyzer: analyzer.New(),
	}
}

// Output formats and outputs the workload information
func (f *Formatter) Output(workloads []types.WorkloadInfo) error {
	switch f.options.OutputFormat {
	case "json":
		return f.outputJSON(workloads)
	case "yaml":
		return f.outputYAML(workloads)
	default:
		return f.outputTable(workloads)
	}
}

// outputJSON outputs workloads in JSON format
func (f *Formatter) outputJSON(workloads []types.WorkloadInfo) error {
	data, err := json.MarshalIndent(workloads, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// outputYAML outputs workloads in YAML format
func (f *Formatter) outputYAML(workloads []types.WorkloadInfo) error {
	data, err := yaml.Marshal(workloads)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// outputTable outputs workloads in table format
func (f *Formatter) outputTable(workloads []types.WorkloadInfo) error {
	for i, workload := range workloads {
		if i > 0 {
			fmt.Println() // Add blank line between workloads
		}

		if err := f.formatWorkload(workload); err != nil {
			return err
		}
	}
	return nil
}

// formatWorkload formats a single workload
func (f *Formatter) formatWorkload(workload types.WorkloadInfo) error {
	// Sort pods if requested
	f.sortPods(workload.Pods)

	// Print workload header
	f.printWorkloadHeader(workload)

	if f.options.Brief {
		// Brief mode: just show summary table
		return f.printBriefSummary(workload)
	}

	// Print summary
	f.printSummary(workload)

	// Check if this is a single pod to avoid redundant headers
	isSinglePod := workload.Kind == "Pod" && len(workload.Pods) == 1

	// Print each pod
	for _, pod := range workload.Pods {
		if err := f.formatPodWithContext(pod, isSinglePod); err != nil {
			return err
		}
	}

	return nil
}

// printWorkloadHeader prints the workload header
func (f *Formatter) printWorkloadHeader(workload types.WorkloadInfo) {
	healthIcon := f.analyzer.GetHealthIcon(workload.Health.Level)
	healthColor := f.getHealthColor(workload.Health.Level)

	headerColor := color.New(color.FgCyan, color.Bold)

	// For single pods, show container count instead of replicas
	replicasInfo := workload.Replicas
	if workload.Kind == "Pod" && len(workload.Pods) == 1 {
		pod := workload.Pods[0]
		// Only count regular containers (not init containers) to match kubectl behavior
		totalContainers := len(pod.Containers)
		readyContainers := f.getReadyCount(pod)
		replicasInfo = fmt.Sprintf("CONTAINERS: %d/%d", readyContainers, totalContainers)
	} else {
		replicasInfo = fmt.Sprintf("REPLICAS: %s", workload.Replicas)
	}

	// For single pods, include NODE and AGE in the header to avoid redundancy
	if workload.Kind == "Pod" && len(workload.Pods) == 1 {
		pod := workload.Pods[0]
		fmt.Printf("%s: %s   %s   NODE: %s   AGE: %s   NAMESPACE: %s\n",
			headerColor.Sprintf("%s", strings.ToUpper(workload.Kind)),
			headerColor.Sprintf("%s", workload.Name),
			replicasInfo,
			pod.NodeName,
			f.formatDuration(pod.Age),
			workload.Namespace,
		)
	} else {
		fmt.Printf("%s: %s   %s   NAMESPACE: %s\n",
			headerColor.Sprintf("%s", strings.ToUpper(workload.Kind)),
			headerColor.Sprintf("%s", workload.Name),
			replicasInfo,
			workload.Namespace,
		)
	}

	fmt.Printf("%s HEALTH: %s (%s)\n\n",
		healthIcon,
		healthColor.Sprintf("%s", workload.Health.Level),
		workload.Health.Reason,
	)
}

// printSummary prints the workload summary
func (f *Formatter) printSummary(workload types.WorkloadInfo) {
	running := 0
	warning := 0
	failed := 0
	totalRestarts := int32(0)

	containerNames := make(map[string]bool)

	for _, pod := range workload.Pods {
		switch pod.Health.Level {
		case string(types.HealthLevelHealthy):
			running++
		case string(types.HealthLevelDegraded):
			warning++
		case string(types.HealthLevelCritical):
			failed++
		}

		// Collect container names
		for _, container := range pod.InitContainers {
			containerNames[fmt.Sprintf("[init] %s", container.Name)] = true
			totalRestarts += container.RestartCount
		}
		for _, container := range pod.Containers {
			containerNames[container.Name] = true
			totalRestarts += container.RestartCount
		}
	}

	fmt.Println("SUMMARY:")
	if f.options.Problematic {
		fmt.Printf("  • %d Problematic pods shown\n", len(workload.Pods))
	} else {
		fmt.Printf("  • %d Pods matched\n", len(workload.Pods))
	}
	fmt.Printf("  • %d Running, %d Warning, %d Failed\n", running, warning, failed)

	// Format container names
	var names []string
	for name := range containerNames {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("  • Containers: %s\n", strings.Join(names, ", "))
	fmt.Printf("  • Total Restarts: %d\n\n", totalRestarts)
}

// printBriefSummary prints just a summary table
func (f *Formatter) printBriefSummary(workload types.WorkloadInfo) error {
	// Show problematic pods info if filtered view
	if f.options.Problematic && len(workload.Pods) > 0 {
		fmt.Printf("%d problematic pods shown\n\n", len(workload.Pods))
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"POD", "STATUS", "READY", "RESTARTS", "AGE"})
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)

	for _, pod := range workload.Pods {
		ready := f.getReadyCount(pod)
		totalContainers := len(pod.Containers)
		age := f.formatDuration(pod.Age)

		statusIcon := f.analyzer.GetHealthIcon(pod.Health.Level)
		status := fmt.Sprintf("%s %s", statusIcon, pod.Health.Level)

		totalRestarts := int32(0)
		for _, container := range append(pod.InitContainers, pod.Containers...) {
			totalRestarts += container.RestartCount
		}

		table.Append([]string{
			pod.Name,
			status,
			fmt.Sprintf("%d/%d", ready, totalContainers),
			fmt.Sprintf("%d", totalRestarts),
			age,
		})
	}

	table.Render()
	return nil
}

// formatPodWithContext formats a single pod with context about whether it's part of a single-pod workload
func (f *Formatter) formatPodWithContext(pod types.PodInfo, isSinglePod bool) error {
	// For multi-pod workloads, print pod header to distinguish between pods
	if !isSinglePod {
		f.printPodHeader(pod)
	}

	// Print container status table
	if err := f.printContainerTable(pod); err != nil {
		return err
	}

	// Print detailed container information if not brief
	if !f.options.Brief {
		for _, container := range pod.InitContainers {
			f.printContainerDetails(container)
		}
		for _, container := range pod.Containers {
			f.printContainerDetails(container)
		}

		// Print recent events
		if f.options.ShowEvents || len(pod.Events) > 0 {
			f.printEvents(pod.Events)
		}
	}

	fmt.Println() // Add spacing between pods
	return nil
}

// printPodHeader prints the pod header
func (f *Formatter) printPodHeader(pod types.PodInfo) {
	healthIcon := f.analyzer.GetHealthIcon(pod.Health.Level)
	healthColor := f.getHealthColor(pod.Health.Level)

	fmt.Printf("POD: %s   NODE: %s   AGE: %s\n",
		color.New(color.Bold).Sprintf("%s", pod.Name),
		pod.NodeName,
		f.formatDuration(pod.Age),
	)

	fmt.Printf("%s HEALTH: %s (%s)\n\n",
		healthIcon,
		healthColor.Sprintf("%s", pod.Health.Level),
		pod.Health.Reason,
	)
}

// printContainerTable prints the container status table
func (f *Formatter) printContainerTable(pod types.PodInfo) error {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"CONTAINER", "STATUS", "RESTARTS", "LAST STATE", "EXIT CODE"})
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)

	// Add init containers
	for _, container := range pod.InitContainers {
		f.addContainerRow(table, container)
	}

	// Add regular containers
	for _, container := range pod.Containers {
		f.addContainerRow(table, container)
	}

	table.Render()
	fmt.Println()
	return nil
}

// addContainerRow adds a container row to the table
func (f *Formatter) addContainerRow(table *tablewriter.Table, container types.ContainerInfo) {
	name := container.Name
	if container.Type == string(types.ContainerTypeInit) {
		name = fmt.Sprintf("[init] %s", container.Name)
	}

	statusIcon := f.analyzer.GetStatusIcon(container.Status)
	status := container.Status
	if !f.options.NoColor {
		status = fmt.Sprintf("%s %s", statusIcon, container.Status)
	}

	exitCode := "-"
	if container.ExitCode != nil {
		exitCode = fmt.Sprintf("%d", *container.ExitCode)
		if *container.ExitCode != 0 && !f.options.NoColor {
			exitCode = color.RedString(exitCode)
		}
	}

	table.Append([]string{
		name,
		status,
		fmt.Sprintf("%d", container.RestartCount),
		container.LastState,
		exitCode,
	})
}

// printContainerDetails prints detailed container information
func (f *Formatter) printContainerDetails(container types.ContainerInfo) {
	gearIcon := "⚙️"
	statusIcon := f.analyzer.GetStatusIcon(container.Status)

	// Add [init] prefix for init containers
	containerName := container.Name
	if container.Type == string(types.ContainerTypeInit) {
		containerName = fmt.Sprintf("[init] %s", container.Name)
	}

	fmt.Printf("%s  Container: %s\n", gearIcon, color.New(color.Bold).Sprintf("%s", containerName))

	// Status
	statusStr := fmt.Sprintf("%s %s", statusIcon, container.Status)
	if container.StartedAt != nil {
		statusStr += fmt.Sprintf(" (started %s ago)", f.formatDuration(time.Since(*container.StartedAt)))
	}
	fmt.Printf("  • Status:      %s\n", statusStr)

	// Image
	fmt.Printf("  • Image:       %s\n", container.Image)

	// Resources
	f.printResourceUsage(container.Resources)

	// Probes
	f.printProbes(container.Probes)

	// Volumes (if wide mode)
	if f.options.Wide && len(container.Volumes) > 0 {
		f.printVolumes(container.Volumes)
	}

	// Environment variables (if requested)
	if f.options.ShowEnv && len(container.Environment) > 0 {
		f.printEnvironment(container.Environment)
	}

	// Special handling for terminated containers
	if container.Status == string(types.ContainerStatusTerminated) || container.RestartCount > 0 {
		if container.ExitCode != nil {
			fmt.Printf("  • Last Exit:   %s (exit code: %d)\n", container.TerminationReason, *container.ExitCode)
		}
		if container.RestartCount > 0 {
			restartInfo := fmt.Sprintf("  • Restart Count: %d", container.RestartCount)
			if container.LastRestartTime != nil {
				restartInfo += fmt.Sprintf(" (last restart: %s ago)", f.formatDuration(time.Since(*container.LastRestartTime)))
			}
			fmt.Printf("%s\n", restartInfo)
		}
	}

	fmt.Println()
}

// printResourceUsage prints resource usage with progress bars
func (f *Formatter) printResourceUsage(resources types.ResourceInfo) {
	fmt.Printf("  • Resources:   ")

	// CPU
	cpuBar := f.createProgressBar(resources.CPUPercentage)
	cpuColor := f.getResourceColor(resources.CPUPercentage)
	fmt.Printf("CPU: %s %.0f%% (%s/%s)\n",
		cpuColor.Sprintf("%s", cpuBar),
		resources.CPUPercentage,
		resources.CPUUsage,
		resources.CPULimit)

	fmt.Printf("                 ")

	// Memory
	memBar := f.createProgressBar(resources.MemPercentage)
	memColor := f.getResourceColor(resources.MemPercentage)
	memWarning := ""
	if resources.MemPercentage > 80 {
		memWarning = " ⚠️"
	}
	fmt.Printf("Mem: %s %.0f%% (%s/%s)%s\n",
		memColor.Sprintf("%s", memBar),
		resources.MemPercentage,
		resources.MemUsage,
		resources.MemLimit,
		memWarning)
}

// printProbes prints probe information
func (f *Formatter) printProbes(probes types.ProbeInfo) {
	if probes.Liveness.Configured {
		icon := f.analyzer.GetProbeIcon(probes.Liveness.Passing, true)
		fmt.Printf("  • Liveness:    %s %s %s on port %s (",
			icon, probes.Liveness.Type, probes.Liveness.Path, probes.Liveness.Port)
		if probes.Liveness.Passing {
			fmt.Printf("passing)\n")
		} else {
			fmt.Printf("failing)\n")
		}
	}

	if probes.Readiness.Configured {
		icon := f.analyzer.GetProbeIcon(probes.Readiness.Passing, true)
		fmt.Printf("  • Readiness:   %s %s %s on port %s (",
			icon, probes.Readiness.Type, probes.Readiness.Path, probes.Readiness.Port)
		if probes.Readiness.Passing {
			fmt.Printf("passing)\n")
		} else {
			fmt.Printf("failing)\n")
		}
	}
}

// printVolumes prints volume information
func (f *Formatter) printVolumes(volumes []types.VolumeInfo) {
	fmt.Printf("  • Volumes:     \n")
	for _, volume := range volumes {
		fmt.Printf("    - %s → %s (%s)\n", volume.MountPath, volume.Details, volume.VolumeType)
	}
}

// printEnvironment prints environment variables
func (f *Formatter) printEnvironment(env []types.EnvVar) {
	fmt.Printf("  • Environment: \n")
	for i, envVar := range env {
		if i >= 5 { // Limit to first 5
			fmt.Printf("    ... and %d more\n", len(env)-5)
			break
		}
		value := envVar.Value
		if envVar.Masked {
			value = "***"
		}
		fmt.Printf("    - %s=%s\n", envVar.Name, value)
	}
}

// printEvents prints recent events
func (f *Formatter) printEvents(events []types.EventInfo) {
	// Determine the time window message based on whether events flag is used
	timeWindow := "last 5m"
	if f.options.ShowEvents {
		timeWindow = "last 1h"
	}

	fmt.Printf("📋 Recent Events (%s):\n", timeWindow)

	if len(events) == 0 {
		fmt.Printf("  • No events found in %s\n", timeWindow)
	} else {
		for _, event := range events {
			age := time.Since(event.Time)
			eventType := ""
			if event.Type == "Warning" {
				eventType = "⚠️  "
			} else if event.Type == "Normal" {
				eventType = "ℹ️  "
			}
			fmt.Printf("  • %s%s: %s (%s)\n", eventType, f.formatDuration(age), event.Message, event.Reason)
		}
	}
	fmt.Println()
}

// Helper functions

// sortPods sorts pods based on the sort option
func (f *Formatter) sortPods(pods []types.PodInfo) {
	switch f.options.SortBy {
	case string(types.SortByName):
		sort.Slice(pods, func(i, j int) bool {
			return pods[i].Name < pods[j].Name
		})
	case string(types.SortByAge):
		sort.Slice(pods, func(i, j int) bool {
			return pods[i].Age > pods[j].Age
		})
	case string(types.SortByRestarts):
		sort.Slice(pods, func(i, j int) bool {
			restartsI := int32(0)
			restartsJ := int32(0)
			for _, c := range append(pods[i].InitContainers, pods[i].Containers...) {
				restartsI += c.RestartCount
			}
			for _, c := range append(pods[j].InitContainers, pods[j].Containers...) {
				restartsJ += c.RestartCount
			}
			return restartsI > restartsJ
		})
	}
}

// getReadyCount returns the number of ready containers
func (f *Formatter) getReadyCount(pod types.PodInfo) int {
	ready := 0
	for _, container := range pod.Containers {
		if container.Ready {
			ready++
		}
	}
	return ready
}

// formatDuration formats a duration in human-readable format
func (f *Formatter) formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// createProgressBar creates a progress bar string
func (f *Formatter) createProgressBar(percentage float64) string {
	if f.options.NoColor {
		return fmt.Sprintf("%.0f%%", percentage)
	}

	segments := 10
	filled := int(percentage / 10)
	if filled > segments {
		filled = segments
	}

	bar := strings.Repeat("▓", filled) + strings.Repeat("░", segments-filled)
	return bar
}

// getHealthColor returns the appropriate color for health status
func (f *Formatter) getHealthColor(level string) *color.Color {
	if f.options.NoColor {
		return color.New()
	}

	switch level {
	case string(types.HealthLevelHealthy):
		return color.New(color.FgGreen)
	case string(types.HealthLevelDegraded):
		return color.New(color.FgYellow)
	case string(types.HealthLevelCritical):
		return color.New(color.FgRed)
	default:
		return color.New()
	}
}

// getResourceColor returns the appropriate color for resource usage
func (f *Formatter) getResourceColor(percentage float64) *color.Color {
	if f.options.NoColor {
		return color.New()
	}

	if percentage > 90 {
		return color.New(color.FgRed)
	} else if percentage > 80 {
		return color.New(color.FgYellow)
	}
	return color.New()
}
