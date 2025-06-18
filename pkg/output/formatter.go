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
	"golang.org/x/term"
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

	// Check if this is a single pod to determine display mode
	isSinglePod := workload.Kind == "Pod" && len(workload.Pods) == 1

	if isSinglePod {
		// Single pod: use detailed view (existing behavior)
		f.printSummary(workload)
		for _, pod := range workload.Pods {
			if err := f.formatPodWithContext(pod, true); err != nil {
				return err
			}
		}
	} else {
		// Multi-pod workload: use enhanced table view
		f.printWorkloadSummary(workload)
		f.printWorkloadTable(workload)

		// Show aggregated events if requested
		if f.options.ShowEvents {
			f.printWorkloadEvents(workload)
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

	// Create a visual separator line
	separatorColor := color.New(color.FgHiBlack)
	separator := separatorColor.Sprint(strings.Repeat("─", 60))

	// Enhanced header with better visual hierarchy
	fmt.Println(separator)

	// For single pods, include NODE and AGE in the header to avoid redundancy
	if workload.Kind == "Pod" && len(workload.Pods) == 1 {
		pod := workload.Pods[0]
		fmt.Printf("🎯 %s: %s   %s   📍 NODE: %s   ⏰ AGE: %s   🏷️  NAMESPACE: %s\n",
			headerColor.Sprintf("%s", strings.ToUpper(workload.Kind)),
			headerColor.Sprintf("%s", workload.Name),
			replicasInfo,
			pod.NodeName,
			f.formatDuration(pod.Age),
			workload.Namespace,
		)
	} else {
		fmt.Printf("🎯 %s: %s   %s   🏷️  NAMESPACE: %s\n",
			headerColor.Sprintf("%s", strings.ToUpper(workload.Kind)),
			headerColor.Sprintf("%s", workload.Name),
			replicasInfo,
			workload.Namespace,
		)
	}

	// Enhanced health status with box drawing characters for emphasis
	healthBorder := "┌─ HEALTH STATUS ──────────────────────────────────────┐"
	healthBottom := "└─────────────────────────────────────────────────────┘"

	fmt.Println(separatorColor.Sprint(healthBorder))
	fmt.Printf("│ %s %s %s (%s) %s│\n",
		healthIcon,
		healthColor.Sprintf("%-10s", strings.ToUpper(workload.Health.Level)),
		healthColor.Sprintf("%-35s", workload.Health.Reason),
		getHealthEmoji(workload.Health.Level),
		strings.Repeat(" ", max(0, 8-len(getHealthEmoji(workload.Health.Level)))),
	)
	fmt.Println(separatorColor.Sprint(healthBottom))
	fmt.Println()
}

// getHealthEmoji returns an additional emoji for health status
func getHealthEmoji(level string) string {
	switch level {
	case string(types.HealthLevelHealthy):
		return "💚"
	case string(types.HealthLevelDegraded):
		return "⚠️"
	case string(types.HealthLevelCritical):
		return "🚨"
	default:
		return "❓"
	}
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

		lastRestartTime := f.getLastRestartTime(pod)

		table.Append([]string{
			pod.Name,
			status,
			fmt.Sprintf("%d/%d", ready, totalContainers),
			f.formatRestartInfo(totalRestarts, lastRestartTime),
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
		f.formatRestartInfo(container.RestartCount, container.LastRestartTime),
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

	// Command and arguments (if wide mode)
	if f.options.Wide {
		f.printCommand(container.Command, container.Args)
	}

	// Container logs (if requested)
	if f.options.ShowLogs && len(container.Logs) > 0 {
		f.printLogs(container.Logs)
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
		memWarning = " ⚠"
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

	// Determine how many environment variables to show
	var limit int
	if f.options.Wide {
		limit = 20 // Show more when --wide is used
	} else {
		limit = 5 // Default limit for normal view
	}

	for i, envVar := range env {
		if i >= limit {
			fmt.Printf("    ... and %d more\n", len(env)-limit)
			break
		}
		value := envVar.Value
		if envVar.Masked {
			value = "***"
		}
		fmt.Printf("    - %s=%s\n", envVar.Name, value)
	}
}

// printCommand prints container command and arguments
func (f *Formatter) printCommand(command []string, args []string) {
	if len(command) == 0 && len(args) == 0 {
		return
	}

	fmt.Printf("  • Command:     \n")

	// Show command (entrypoint)
	if len(command) > 0 {
		terminalWidth := f.getTerminalWidth()
		indentWidth := 6 // "    - " prefix
		maxLineWidth := terminalWidth - indentWidth

		commandStr := strings.Join(command, " ")
		fmt.Printf("    - Entrypoint: ")
		f.printWrappedCommandLine(commandStr, maxLineWidth-12, indentWidth+12) // 12 = len("Entrypoint: ")
	}

	// Show arguments (cmd)
	if len(args) > 0 {
		terminalWidth := f.getTerminalWidth()
		indentWidth := 6 // "    - " prefix
		maxLineWidth := terminalWidth - indentWidth

		argsStr := strings.Join(args, " ")
		fmt.Printf("    - Args:       ")
		f.printWrappedCommandLine(argsStr, maxLineWidth-12, indentWidth+12) // 12 = len("Args:       ")
	}
}

// printWrappedCommandLine prints a command line with intelligent wrapping
func (f *Formatter) printWrappedCommandLine(line string, maxWidth, indentWidth int) {
	if len(line) <= maxWidth {
		// Line fits, print as-is
		fmt.Printf("%s\n", line)
		return
	}

	// Line is too long, wrap it intelligently
	continuationIndent := strings.Repeat(" ", indentWidth)

	// Print first line
	firstLine := line[:maxWidth]
	// Try to break at a space boundary if possible
	if lastSpace := strings.LastIndex(firstLine, " "); lastSpace > maxWidth*3/4 {
		firstLine = line[:lastSpace]
		line = line[lastSpace+1:] // Skip the space
	} else {
		line = line[maxWidth:]
	}
	fmt.Printf("%s\n", firstLine)

	// Print continuation lines
	for len(line) > 0 {
		if len(line) <= maxWidth {
			fmt.Printf("%s%s\n", continuationIndent, line)
			break
		}

		continuationLine := line[:maxWidth]
		// Try to break at word boundary
		if lastSpace := strings.LastIndex(continuationLine, " "); lastSpace > maxWidth*3/4 {
			continuationLine = line[:lastSpace]
			line = line[lastSpace+1:]
		} else {
			line = line[maxWidth:]
		}
		fmt.Printf("%s%s\n", continuationIndent, continuationLine)
	}
}

// printLogs prints recent container logs
func (f *Formatter) printLogs(logs []string) {
	fmt.Printf("  • Recent Logs:\n")
	if len(logs) == 0 {
		fmt.Printf("    (no logs available)\n")
		return
	}

	terminalWidth := f.getTerminalWidth()
	indentWidth := 4 // "    " prefix
	maxLineWidth := terminalWidth - indentWidth

	for _, logLine := range logs {
		f.printWrappedLogLine(logLine, maxLineWidth, indentWidth)
	}
}

// getTerminalWidth gets the terminal width, with fallback to 120
func (f *Formatter) getTerminalWidth() int {
	// Try to get terminal width from environment or system
	// Default to 120 if unable to determine
	const defaultWidth = 120
	const minWidth = 80

	// Try to detect actual terminal width
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		if width < minWidth {
			return minWidth
		}
		return width
	}

	// Fallback to default
	return defaultWidth
}

// printWrappedLogLine prints a log line with intelligent wrapping
func (f *Formatter) printWrappedLogLine(line string, maxWidth, indentWidth int) {
	if len(line) <= maxWidth {
		// Line fits, print as-is
		fmt.Printf("    %s\n", line)
		return
	}

	// Line is too long, wrap it intelligently
	indent := strings.Repeat(" ", indentWidth)
	continuationIndent := strings.Repeat(" ", indentWidth+2) // Extra 2 spaces for continuation

	// Print first line
	firstLine := line[:maxWidth]
	// Try to break at a word boundary if possible
	if lastSpace := strings.LastIndex(firstLine, " "); lastSpace > maxWidth*3/4 {
		firstLine = line[:lastSpace]
		line = line[lastSpace+1:] // Skip the space
	} else {
		line = line[maxWidth:]
	}
	fmt.Printf("%s%s\n", indent, firstLine)

	// Print continuation lines
	for len(line) > 0 {
		maxContinuationWidth := maxWidth - 2 // Account for continuation indent
		if len(line) <= maxContinuationWidth {
			fmt.Printf("%s%s\n", continuationIndent, line)
			break
		}

		continuationLine := line[:maxContinuationWidth]
		// Try to break at word boundary
		if lastSpace := strings.LastIndex(continuationLine, " "); lastSpace > maxContinuationWidth*3/4 {
			continuationLine = line[:lastSpace]
			line = line[lastSpace+1:]
		} else {
			line = line[maxContinuationWidth:]
		}
		fmt.Printf("%s%s\n", continuationIndent, continuationLine)
	}
}

// printEvents prints recent events
func (f *Formatter) printEvents(events []types.EventInfo) {
	// Determine the time window message based on whether events flag is used
	timeWindow := "last 5m"
	if f.options.ShowEvents {
		timeWindow = "last 1h"
	}

	// Enhanced events section with better visual structure
	eventsColor := color.New(color.FgHiBlue, color.Bold)
	fmt.Printf("📋 %s (%s):\n", eventsColor.Sprint("Recent Events"), timeWindow)

	if len(events) == 0 {
		fmt.Printf("  • ✨ No events found in %s\n", timeWindow)
	} else {
		for _, event := range events {
			age := time.Since(event.Time)
			eventIcon := ""
			eventColor := color.New()

			if event.Type == "Warning" {
				eventIcon = "⚠️" // Warning triangle for warnings
				eventColor = color.New(color.FgYellow, color.Bold)
			} else if event.Type == "Error" {
				eventIcon = "🚨" // Siren for errors
				eventColor = color.New(color.FgRed, color.Bold)
			} else if event.Type == "Normal" {
				eventIcon = "ℹ️" // Info icon
				eventColor = color.New(color.FgCyan)
			} else {
				eventIcon = "📝" // Generic event icon
				eventColor = color.New(color.FgWhite)
			}

			fmt.Printf("  • %s %s %s: %s (%s)\n",
				eventIcon,
				eventColor.Sprint(event.Type),
				f.formatDuration(age),
				event.Message,
				event.Reason)
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

// formatRestartInfo formats restart count with last restart time
func (f *Formatter) formatRestartInfo(restartCount int32, lastRestartTime *time.Time) string {
	if restartCount == 0 {
		return "0"
	}

	restartStr := fmt.Sprintf("%d", restartCount)
	if lastRestartTime != nil {
		restartStr += fmt.Sprintf(" (last %s ago)", f.formatDuration(time.Since(*lastRestartTime)))
	}

	return restartStr
}

// getLastRestartTime returns the most recent restart time from all containers in a pod
func (f *Formatter) getLastRestartTime(pod types.PodInfo) *time.Time {
	var mostRecent *time.Time

	for _, container := range append(pod.InitContainers, pod.Containers...) {
		if container.LastRestartTime != nil {
			if mostRecent == nil || container.LastRestartTime.After(*mostRecent) {
				mostRecent = container.LastRestartTime
			}
		}
	}

	return mostRecent
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
		return color.New(color.FgHiGreen, color.Bold)
	case string(types.HealthLevelDegraded):
		return color.New(color.FgHiYellow, color.Bold)
	case string(types.HealthLevelCritical):
		return color.New(color.FgHiRed, color.Bold)
	default:
		return color.New()
	}
}

// getResourceColor returns the appropriate color for resource usage
func (f *Formatter) getResourceColor(percentage float64) *color.Color {
	if f.options.NoColor {
		return color.New()
	}

	if percentage >= 90 {
		return color.New(color.FgHiRed, color.Bold)
	} else if percentage >= 70 {
		return color.New(color.FgHiYellow, color.Bold)
	}
	return color.New(color.FgHiGreen, color.Bold)
}

// printWorkloadSummary prints enhanced summary for multi-pod workloads
func (f *Formatter) printWorkloadSummary(workload types.WorkloadInfo) {
	running := 0
	warning := 0
	failed := 0
	totalRestarts := int32(0)

	// Collect container information with aggregated data and usage stats
	containerInfo := make(map[string]struct {
		Image       string
		Type        string
		CPURequest  string
		CPULimit    string
		MemRequest  string
		MemLimit    string
		VolumeTypes map[string]bool
		CPUUsages   []float64 // All CPU usage percentages for this container type
		MemUsages   []float64 // All Memory usage percentages for this container type
	})

	for _, pod := range workload.Pods {
		switch pod.Health.Level {
		case string(types.HealthLevelHealthy):
			running++
		case string(types.HealthLevelDegraded):
			warning++
		case string(types.HealthLevelCritical):
			failed++
		}

		// Collect container information
		for _, container := range append(pod.InitContainers, pod.Containers...) {
			totalRestarts += container.RestartCount

			containerName := container.Name
			if container.Type == string(types.ContainerTypeInit) {
				containerName = fmt.Sprintf("[init] %s", container.Name)
			}

			// Use full image URL instead of just the short name
			imageName := container.Image

			// Initialize or update container info
			if info, exists := containerInfo[containerName]; exists {
				// Container already exists, add usage data and update volume types
				info.CPUUsages = append(info.CPUUsages, container.Resources.CPUPercentage)
				info.MemUsages = append(info.MemUsages, container.Resources.MemPercentage)
				for _, volume := range container.Volumes {
					info.VolumeTypes[volume.VolumeType] = true
				}
				containerInfo[containerName] = info
			} else {
				// New container
				volumeTypes := make(map[string]bool)
				for _, volume := range container.Volumes {
					volumeTypes[volume.VolumeType] = true
				}

				containerInfo[containerName] = struct {
					Image       string
					Type        string
					CPURequest  string
					CPULimit    string
					MemRequest  string
					MemLimit    string
					VolumeTypes map[string]bool
					CPUUsages   []float64
					MemUsages   []float64
				}{
					Image:       imageName,
					Type:        container.Type,
					CPURequest:  container.Resources.CPURequest,
					CPULimit:    container.Resources.CPULimit,
					MemRequest:  container.Resources.MemRequest,
					MemLimit:    container.Resources.MemLimit,
					VolumeTypes: volumeTypes,
					CPUUsages:   []float64{container.Resources.CPUPercentage},
					MemUsages:   []float64{container.Resources.MemPercentage},
				}
			}
		}
	}

	fmt.Println("WORKLOAD SUMMARY:")
	if f.options.Problematic {
		fmt.Printf("  • %d Problematic pods shown\n", len(workload.Pods))
	} else {
		fmt.Printf("  • %d Pods: %d Running, %d Warning, %d Failed\n", len(workload.Pods), running, warning, failed)
	}

	// Sort container names for consistent output
	var containerNames []string
	for name := range containerInfo {
		containerNames = append(containerNames, name)
	}
	sort.Strings(containerNames)

	fmt.Printf("  • Containers:\n")
	for i, containerName := range containerNames {
		info := containerInfo[containerName]
		fmt.Printf("        %d) %s\n", i+1, containerName)
		fmt.Printf("           Image: %s\n", info.Image)

		// Format resource allocation
		resourceParts := []string{}
		if info.CPURequest != "" || info.CPULimit != "" {
			cpuInfo := "CPU: "
			if info.CPURequest != "" && info.CPULimit != "" {
				cpuInfo += fmt.Sprintf("%s/%s", info.CPURequest, info.CPULimit)
			} else if info.CPULimit != "" {
				cpuInfo += fmt.Sprintf("limit %s", info.CPULimit)
			} else if info.CPURequest != "" {
				cpuInfo += fmt.Sprintf("request %s", info.CPURequest)
			}
			resourceParts = append(resourceParts, cpuInfo)
		}

		if info.MemRequest != "" || info.MemLimit != "" {
			memInfo := "Memory: "
			if info.MemRequest != "" && info.MemLimit != "" {
				memInfo += fmt.Sprintf("%s/%s", info.MemRequest, info.MemLimit)
			} else if info.MemLimit != "" {
				memInfo += fmt.Sprintf("limit %s", info.MemLimit)
			} else if info.MemRequest != "" {
				memInfo += fmt.Sprintf("request %s", info.MemRequest)
			}
			resourceParts = append(resourceParts, memInfo)
		}

		if len(resourceParts) > 0 {
			fmt.Printf("           Resources: %s\n", strings.Join(resourceParts, ", "))
		} else {
			fmt.Printf("           Resources: No limits/requests set\n")
		}

		// Display resource utilization statistics
		if len(info.CPUUsages) > 0 {
			cpuStats := f.calculateResourceStats(info.CPUUsages)
			memStats := f.calculateResourceStats(info.MemUsages)

			fmt.Printf("           Usage: CPU %s avg:%s %s p90:%s %s p99:%s\n",
				f.createMiniProgressBar(cpuStats.Average), f.formatUsageWithColor(cpuStats.Average),
				f.createMiniProgressBar(cpuStats.P90), f.formatUsageWithColor(cpuStats.P90),
				f.createMiniProgressBar(cpuStats.P99), f.formatUsageWithColor(cpuStats.P99))

			fmt.Printf("                  Mem %s avg:%s %s p90:%s %s p99:%s\n",
				f.createMiniProgressBar(memStats.Average), f.formatUsageWithColor(memStats.Average),
				f.createMiniProgressBar(memStats.P90), f.formatUsageWithColor(memStats.P90),
				f.createMiniProgressBar(memStats.P99), f.formatUsageWithColor(memStats.P99))
		}

		// Show volume types if any
		if len(info.VolumeTypes) > 0 {
			var volumes []string
			for volType := range info.VolumeTypes {
				volumes = append(volumes, volType)
			}
			sort.Strings(volumes)
			fmt.Printf("           Volumes: %s\n", strings.Join(volumes, ", "))
		}

		if i < len(containerNames)-1 {
			fmt.Println()
		}
	}

	fmt.Printf("  • Total Restarts: %d\n\n", totalRestarts)
}

// printWorkloadTable prints a table view of pods in the workload
func (f *Formatter) printWorkloadTable(workload types.WorkloadInfo) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"POD", "NODE", "STATUS", "READY", "RESTARTS", "CPU", "MEMORY", "AGE"})
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)

	for _, pod := range workload.Pods {
		ready := f.getReadyCount(pod)
		totalContainers := len(pod.Containers)
		age := f.formatDuration(pod.Age)

		statusIcon := f.analyzer.GetHealthIcon(pod.Health.Level)
		status := fmt.Sprintf("%s %s", statusIcon, pod.Health.Level)

		totalRestarts := int32(0)
		totalCPU := 0.0
		totalMem := 0.0
		for _, container := range append(pod.InitContainers, pod.Containers...) {
			totalRestarts += container.RestartCount
			totalCPU += container.Resources.CPUPercentage
			totalMem += container.Resources.MemPercentage
		}

		lastRestartTime := f.getLastRestartTime(pod)

		// Truncate node name for better table formatting
		node := pod.NodeName
		if len(node) > 20 {
			node = node[:17] + "..."
		}

		// Format CPU and Memory percentages
		cpuStr := fmt.Sprintf("%.0f%%", totalCPU)
		memStr := fmt.Sprintf("%.0f%%", totalMem)
		if totalCPU > 90 {
			cpuStr = color.RedString(cpuStr)
		} else if totalCPU > 80 {
			cpuStr = color.YellowString(cpuStr)
		}
		if totalMem > 90 {
			memStr = color.RedString(memStr)
		} else if totalMem > 80 {
			memStr = color.YellowString(memStr)
		}

		table.Append([]string{
			pod.Name,
			node,
			status,
			fmt.Sprintf("%d/%d", ready, totalContainers),
			f.formatRestartInfo(totalRestarts, lastRestartTime),
			cpuStr,
			memStr,
			age,
		})
	}

	table.Render()
	fmt.Println()
}

// printWorkloadEvents prints aggregated events for the workload
func (f *Formatter) printWorkloadEvents(workload types.WorkloadInfo) {
	// Collect all events from all pods
	var allEvents []types.EventInfo
	for _, pod := range workload.Pods {
		allEvents = append(allEvents, pod.Events...)
	}

	// Sort events by time (newest first)
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Time.After(allEvents[j].Time)
	})

	// Determine the time window message
	timeWindow := "last 1h"

	// Enhanced workload events section with better visual structure
	eventsColor := color.New(color.FgHiBlue, color.Bold)
	fmt.Printf("📋 %s (%s):\n", eventsColor.Sprint("Workload Events"), timeWindow)

	if len(allEvents) == 0 {
		fmt.Printf("  • ✨ No events found in %s\n", timeWindow)
	} else {
		// Show up to 10 most recent events
		maxEvents := 10
		if len(allEvents) > maxEvents {
			allEvents = allEvents[:maxEvents]
		}

		for _, event := range allEvents {
			age := time.Since(event.Time)
			eventIcon := ""
			eventColor := color.New()

			if event.Type == "Warning" {
				eventIcon = "⚠️" // Warning triangle for warnings
				eventColor = color.New(color.FgYellow, color.Bold)
			} else if event.Type == "Error" {
				eventIcon = "🚨" // Siren for errors
				eventColor = color.New(color.FgRed, color.Bold)
			} else if event.Type == "Normal" {
				eventIcon = "ℹ️" // Info icon
				eventColor = color.New(color.FgCyan)
			} else {
				eventIcon = "📝" // Generic event icon
				eventColor = color.New(color.FgWhite)
			}

			fmt.Printf("  • %s %s %s: %s (%s) [%s]\n",
				eventIcon,
				eventColor.Sprint(event.Type),
				f.formatDuration(age),
				event.Message,
				event.Reason,
				event.PodName)
		}

		if len(workload.Pods) > 0 {
			totalEvents := 0
			for _, pod := range workload.Pods {
				totalEvents += len(pod.Events)
			}
			if totalEvents > maxEvents {
				fmt.Printf("  💭 ... and %d more events\n", totalEvents-maxEvents)
			}
		}
	}
	fmt.Println()
}

// calculateResourceStats calculates resource utilization statistics
func (f *Formatter) calculateResourceStats(usages []float64) struct {
	Average float64
	P90     float64
	P99     float64
} {
	if len(usages) == 0 {
		return struct {
			Average float64
			P90     float64
			P99     float64
		}{0, 0, 0}
	}

	sort.Float64s(usages)

	// Calculate average
	total := float64(0)
	for _, usage := range usages {
		total += usage
	}
	average := total / float64(len(usages))

	// Calculate percentiles
	n := len(usages)
	p90Index := int(float64(n) * 0.9)
	p99Index := int(float64(n) * 0.99)

	// Ensure indices are within bounds
	if p90Index >= n {
		p90Index = n - 1
	}
	if p99Index >= n {
		p99Index = n - 1
	}

	p90 := usages[p90Index]
	p99 := usages[p99Index]

	return struct {
		Average float64
		P90     float64
		P99     float64
	}{
		Average: average,
		P90:     p90,
		P99:     p99,
	}
}

// createMiniProgressBar creates a mini progress bar string
func (f *Formatter) createMiniProgressBar(percentage float64) string {
	if f.options.NoColor {
		return fmt.Sprintf("%.0f%%", percentage)
	}

	// Create a clean gradient bar with simplified modern colors
	segments := 8

	var bar strings.Builder

	// Create gradient based on overall percentage for each segment
	for i := 0; i < segments; i++ {
		segmentThreshold := float64(i+1) * 12.5 // Each segment represents 12.5%

		if percentage >= segmentThreshold {
			// Filled segment - use simplified color scheme
			if percentage >= 90 {
				// Critical: Red (90%+)
				bar.WriteString(color.New(color.FgHiRed, color.Bold).Sprint("█"))
			} else if percentage >= 70 {
				// Warning: Yellow (70-90%)
				bar.WriteString(color.New(color.FgHiYellow, color.Bold).Sprint("█"))
			} else {
				// Healthy: Green (0-70%)
				bar.WriteString(color.New(color.FgHiGreen, color.Bold).Sprint("█"))
			}
		} else if percentage >= segmentThreshold-12.5 {
			// Partially filled segment - same color scheme
			if percentage >= 90 {
				bar.WriteString(color.New(color.FgHiRed).Sprint("▓"))
			} else if percentage >= 70 {
				bar.WriteString(color.New(color.FgHiYellow).Sprint("▓"))
			} else {
				bar.WriteString(color.New(color.FgHiGreen).Sprint("▓"))
			}
		} else {
			// Empty segment - subtle gray
			bar.WriteString(color.New(color.FgHiBlack).Sprint("░"))
		}
	}

	return bar.String()
}

// formatUsageWithColor formats a usage percentage with appropriate color
func (f *Formatter) formatUsageWithColor(percentage float64) string {
	if f.options.NoColor {
		return fmt.Sprintf("%.0f%%", percentage)
	}

	if percentage >= 90 {
		return color.New(color.FgHiRed, color.Bold).Sprintf("%.0f%%", percentage)
	} else if percentage >= 70 {
		return color.New(color.FgHiYellow, color.Bold).Sprintf("%.0f%%", percentage)
	}
	return color.New(color.FgHiGreen, color.Bold).Sprintf("%.0f%%", percentage)
}
