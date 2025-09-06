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

	// Show logs warning for single pods
	if workload.Kind == "Pod" && f.options.ShowLogs {
		f.printLogsWarning()
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
		f.printWorkloadEvents(workload)
	} else {
		// Multi-pod workload: use enhanced table view
		f.printWorkloadSummary(workload)
		f.printWorkloadTable(workload)

		// Show aggregated events if requested
		f.printWorkloadEvents(workload)
	}

	return nil
}

// printWorkloadHeader prints the workload header
func (f *Formatter) printWorkloadHeader(workload types.WorkloadInfo) {
	healthIcon := f.analyzer.GetHealthIcon(workload.Health.Level)
	healthColor := f.getHealthColor(workload.Health.Level)

	headerColor := color.New(color.FgCyan, color.Bold)

	// For single pods, show container count instead of replicas
	var replicasInfo string
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

		// Build the header with optional service account
		baseInfo := fmt.Sprintf("🎯 %s: %s   %s   📍 NODE: %s   ⏰ AGE: %s   🏷️  NAMESPACE: %s",
			headerColor.Sprintf("%s", strings.ToUpper(workload.Kind)),
			headerColor.Sprintf("%s", workload.Name),
			replicasInfo,
			pod.NodeName,
			f.formatDuration(pod.Age),
			workload.Namespace,
		)

		// Add service account if present and not default
		if pod.ServiceAccount != "" && pod.ServiceAccount != "default" {
			fmt.Printf("%s   🔐 SERVICE ACCOUNT: %s\n", baseInfo, pod.ServiceAccount)
		} else {
			fmt.Printf("%s\n", baseInfo)
		}

		// Add network information for single pods
		f.printNetworkInfo(pod)
	} else {
		// For multi-pod workloads, determine network type from the first pod
		networkInfo := ""
		if len(workload.Pods) > 0 {
			firstPod := workload.Pods[0]
			networkType := "Pod"
			if firstPod.Network.HostNetwork {
				networkType = "Host"
			}
			networkInfo = fmt.Sprintf("   🌐 NETWORK: %s", networkType)
		}

		fmt.Printf("🎯 %s: %s   %s   🏷️  NAMESPACE: %s%s\n",
			headerColor.Sprintf("%s", strings.ToUpper(workload.Kind)),
			headerColor.Sprintf("%s", workload.Name),
			replicasInfo,
			workload.Namespace,
			networkInfo,
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

	f.printPodMetadata(pod)

	for _, container := range pod.InitContainers {
		f.printContainerDetails(container)
	}
	for _, container := range pod.Containers {
		f.printContainerDetails(container)
	}

	fmt.Println() // Add spacing between pods
	return nil
}

// printPodHeader prints the pod header
func (f *Formatter) printPodHeader(pod types.PodInfo) {
	healthIcon := f.analyzer.GetHealthIcon(pod.Health.Level)
	healthColor := f.getHealthColor(pod.Health.Level)

	// Build pod header with status, optional service account
	statusColor := f.getPodStatusColor(pod.Status)
	baseInfo := fmt.Sprintf("POD: %s   STATUS: %s   NODE: %s   AGE: %s",
		color.New(color.Bold).Sprintf("%s", pod.Name),
		statusColor.Sprintf("%s", pod.Status),
		pod.NodeName,
		f.formatDuration(pod.Age),
	)

	// Add service account if present and not default
	if pod.ServiceAccount != "" && pod.ServiceAccount != "default" {
		fmt.Printf("%s   SERVICE ACCOUNT: %s\n", baseInfo, pod.ServiceAccount)
	} else {
		fmt.Printf("%s\n", baseInfo)
	}

	// Add network information
	f.printNetworkInfo(pod)

	fmt.Printf("%s HEALTH: %s (%s)\n",
		healthIcon,
		healthColor.Sprintf("%s", pod.Health.Level),
		pod.Health.Reason,
	)

	// Show conditions for pending pods or if there are failed conditions
	f.printPodConditions(pod)
	fmt.Println()
}

// printContainerTable prints the container status table
func (f *Formatter) printContainerTable(pod types.PodInfo) error {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"CONTAINER", "STATUS", "RESTARTS", "LAST STATE", "EXIT CODE"})
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)

	// Configure table formatting for better width handling
	f.configureContainerTableWidths(table)

	// Add init containers
	for _, container := range pod.InitContainers {
		if f.shouldShowContainer(container.Name) {
			f.addContainerRow(table, container)
		}
	}

	// Add regular containers
	for _, container := range pod.Containers {
		if f.shouldShowContainer(container.Name) {
			f.addContainerRow(table, container)
		}
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

	// Format last state with reason if available
	lastState := container.LastState
	if container.LastStateReason != "" && container.LastState != "None" {
		lastState = fmt.Sprintf("%s (%s)", container.LastState, container.LastStateReason)
	}

	table.Append([]string{
		name,
		status,
		f.formatRestartInfo(container.RestartCount, container.LastRestartTime),
		lastState,
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

	// Ports
	if len(container.Ports) > 0 {
		f.printPorts(container.Ports)
	}

	if len(container.Volumes) > 0 {
		f.printVolumes(container.Volumes)
	}

	// Environment variables (if requested)
	if len(container.Environment) > 0 {
		f.printEnvironment(container.Environment)
	}

	// Command and arguments
	f.printCommand(container.Command, container.Args)

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
			// Add last restart reason if available
			if container.LastStateReason != "" && container.LastState != "None" {
				restartInfo += fmt.Sprintf(" - reason: %s", container.LastStateReason)
			}
			fmt.Printf("%s\n", restartInfo)
		}
	}

	fmt.Println()
}

// printPorts prints container port information
func (f *Formatter) printPorts(ports []types.PortInfo) {
	fmt.Printf("  • Ports:       \n")
	for _, p := range ports {
		desc := fmt.Sprintf("%d/%s", p.ContainerPort, strings.ToUpper(p.Protocol))
		if p.HostPort != 0 {
			desc += fmt.Sprintf(" (host:%d)", p.HostPort)
		}
		if p.Name != "" {
			fmt.Printf("    - %s: %s\n", p.Name, desc)
		} else {
			fmt.Printf("    - %s\n", desc)
		}
	}
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
	limit := 20

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

		fmt.Printf("    - Args:       ")
		for i, arg := range args {
			if i > 0 {
				// Indent subsequent args to the same column as the first argument
				fmt.Print(strings.Repeat(" ", indentWidth+12))
			}
			f.printWrappedCommandLine(arg, maxLineWidth-12, indentWidth+12) // 12 = len("Args:       ")
		}
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
	timeWindow := "last 1h"

	// Enhanced events section with better visual structure
	eventsColor := color.New(color.FgHiBlue, color.Bold)
	fmt.Printf("📋 %s (%s):\n", eventsColor.Sprint("Recent Events"), timeWindow)

	if len(events) == 0 {
		fmt.Printf("  • ✨ No events found in %s\n", timeWindow)
	} else {
		// Sort events with FailedScheduling first, then by time
		sortedEvents := make([]types.EventInfo, len(events))
		copy(sortedEvents, events)

		sort.Slice(sortedEvents, func(i, j int) bool {
			// Prioritize FailedScheduling events
			iIsScheduling := sortedEvents[i].Reason == "FailedScheduling"
			jIsScheduling := sortedEvents[j].Reason == "FailedScheduling"

			if iIsScheduling && !jIsScheduling {
				return true
			}
			if !iIsScheduling && jIsScheduling {
				return false
			}

			// If both or neither are FailedScheduling, sort by time (newest first)
			return sortedEvents[i].Time.After(sortedEvents[j].Time)
		})

		for _, event := range sortedEvents {
			age := time.Since(event.Time)
			eventIcon := ""
			eventColor := color.New()

			// Special handling for FailedScheduling events
			if event.Reason == "FailedScheduling" {
				eventIcon = "🚫" // Blocked icon for scheduling failures
				eventColor = color.New(color.FgRed, color.Bold)
			} else if event.Type == "Warning" {
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

			// Format the message for FailedScheduling to be more readable
			message := event.Message
			if event.Reason == "FailedScheduling" && len(message) > 100 {
				// Wrap long scheduling messages intelligently
				message = f.wrapSchedulingMessage(message)
			}

			fmt.Printf("  • %s %s %s: %s (%s)\n",
				eventIcon,
				eventColor.Sprint(event.Type),
				f.formatDuration(age),
				message,
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
		CPUValues   []string  // All CPU usage values (e.g., "70m", "100m")
		MemValues   []string  // All Memory usage values (e.g., "14Mi", "256Mi")
		Status      string
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
			// Skip containers that don't match the filter
			if !f.shouldShowContainer(container.Name) {
				continue
			}

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
				info.CPUValues = append(info.CPUValues, container.Resources.CPUUsage)
				info.MemValues = append(info.MemValues, container.Resources.MemUsage)
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
					CPUValues   []string
					MemValues   []string
					Status      string
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
					CPUValues:   []string{container.Resources.CPUUsage},
					MemValues:   []string{container.Resources.MemUsage},
					Status:      container.Status,
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

			// Calculate actual values for percentiles
			cpuAvgValue := f.calculateAverageValue(info.CPUValues)
			cpuP90Value := f.calculatePercentileValue(info.CPUValues, 0.9)
			cpuP99Value := f.calculatePercentileValue(info.CPUValues, 0.99)

			memAvgValue := f.calculateAverageValue(info.MemValues)
			memP90Value := f.calculatePercentileValue(info.MemValues, 0.9)
			memP99Value := f.calculatePercentileValue(info.MemValues, 0.99)

			if info.Status == string(types.ContainerStatusRunning) {
				fmt.Printf("           Usage: CPU %s avg:%s (%s) %s p90:%s (%s) %s p99:%s (%s)\n",
					f.createMiniProgressBar(cpuStats.Average), f.formatUsageWithColor(cpuStats.Average), cpuAvgValue,
					f.createMiniProgressBar(cpuStats.P90), f.formatUsageWithColor(cpuStats.P90), cpuP90Value,
					f.createMiniProgressBar(cpuStats.P99), f.formatUsageWithColor(cpuStats.P99), cpuP99Value)

				fmt.Printf("                  Mem %s avg:%s (%s) %s p90:%s (%s) %s p99:%s (%s)\n",
					f.createMiniProgressBar(memStats.Average), f.formatUsageWithColor(memStats.Average), memAvgValue,
					f.createMiniProgressBar(memStats.P90), f.formatUsageWithColor(memStats.P90), memP90Value,
					f.createMiniProgressBar(memStats.P99), f.formatUsageWithColor(memStats.P99), memP99Value)
			}
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
	headers := []string{"POD", "NODE", "STATUS", "READY", "RESTARTS", "CPU (cores)", "MEMORY", "IP", "AGE"}
	table.SetHeader(headers)
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)

	// Configure column widths based on content and terminal size
	f.configureWorkloadTableWidths(table, workload)

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

		// Safely read metrics (pod.Metrics may be nil)
		cpuUsage := "-"
		memoryUsage := "-"
		if pod.Metrics != nil {
			if pod.Metrics.CPUUsage != "" {
				cpuUsage = pod.Metrics.CPUUsage
			}
			if pod.Metrics.MemoryUsage != "" {
				memoryUsage = pod.Metrics.MemoryUsage
			}
		}

		// Use full node name - column width will be calculated dynamically
		node := pod.NodeName

		// Get primary IP (first PodIP or fallback to PodIP field)
		primaryIP := "-"
		if len(pod.Network.PodIPs) > 0 {
			primaryIP = pod.Network.PodIPs[0]
		} else if pod.Network.PodIP != "" {
			primaryIP = pod.Network.PodIP
		}

		table.Append([]string{
			pod.Name,
			node,
			status,
			fmt.Sprintf("%d/%d", ready, totalContainers),
			f.formatRestartInfo(totalRestarts, lastRestartTime),
			cpuUsage,
			memoryUsage,
			primaryIP,
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

			fmt.Printf("  • %s %s %s [%s]: %s (%s)\n",
				eventIcon,
				eventColor.Sprint(event.Type),
				f.formatDuration(age),
				event.PodName,
				event.Message,
				event.Reason)
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

// configureWorkloadTableWidths configures optimal column widths for the workload table
func (f *Formatter) configureWorkloadTableWidths(table *tablewriter.Table, workload types.WorkloadInfo) {
	if len(workload.Pods) == 0 {
		return
	}

	// Get terminal width
	terminalWidth := f.getTerminalWidth()

	// Set table formatting options for better width handling
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)

	// Calculate if we need to adjust node names based on available space
	// If terminal is wide enough, don't truncate node names
	// Only truncate if terminal is very narrow
	if terminalWidth < 100 {
		// For narrow terminals, we'll let the natural wrapping handle it
		table.SetColMinWidth(0, 15) // POD column minimum
		table.SetColMinWidth(1, 15) // NODE column minimum
	} else {
		// For wider terminals, allow more space
		table.SetColMinWidth(0, 25) // POD column minimum
		table.SetColMinWidth(1, 25) // NODE column minimum
	}

	// Set column alignments
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,   // POD
		tablewriter.ALIGN_LEFT,   // NODE
		tablewriter.ALIGN_LEFT,   // STATUS
		tablewriter.ALIGN_CENTER, // READY
		tablewriter.ALIGN_LEFT,   // RESTARTS
		tablewriter.ALIGN_LEFT,   // CPU (cores)
		tablewriter.ALIGN_LEFT,   // MEMORY
		tablewriter.ALIGN_LEFT,   // IP
		tablewriter.ALIGN_RIGHT,  // AGE
	})
}

// configureContainerTableWidths configures optimal column widths for the container table
func (f *Formatter) configureContainerTableWidths(table *tablewriter.Table) {
	terminalWidth := f.getTerminalWidth()

	// Set table formatting options
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)

	// Set minimum column widths based on terminal size
	if terminalWidth < 100 {
		table.SetColMinWidth(0, 15) // CONTAINER column
		table.SetColMinWidth(3, 15) // LAST STATE column
	} else {
		table.SetColMinWidth(0, 20) // CONTAINER column
		table.SetColMinWidth(3, 20) // LAST STATE column
	}

	// Set column alignments
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,   // CONTAINER
		tablewriter.ALIGN_LEFT,   // STATUS
		tablewriter.ALIGN_LEFT,   // RESTARTS
		tablewriter.ALIGN_LEFT,   // LAST STATE
		tablewriter.ALIGN_CENTER, // EXIT CODE
	})
}

// printLogsWarning prints a warning message when logs are being displayed
func (f *Formatter) printLogsWarning() {
	warningColor := color.New(color.FgYellow, color.Bold)
	separatorColor := color.New(color.FgHiBlack)

	// Create a visual warning box
	warningBox := "┌─ WARNING ────────────────────────────────────────────┐"
	warningBottom := "└─────────────────────────────────────────────────────┘"

	fmt.Println(separatorColor.Sprint(warningBox))
	fmt.Printf("│ %s %s │\n",
		warningColor.Sprint("⚠️  SHOWING CONTAINER LOGS"),
		strings.Repeat(" ", max(0, 24)), // Padding to align with box
	)
	fmt.Printf("│ %s %s │\n",
		"Recent container logs are included below.",
		strings.Repeat(" ", max(0, 12)), // Padding to align with box
	)
	fmt.Println(separatorColor.Sprint(warningBottom))
	fmt.Println()
}

// printPodMetadata prints pod metadata (labels and annotations)
func (f *Formatter) printPodMetadata(pod types.PodInfo) {
	// Print labels
	if len(pod.Labels) > 0 {
		fmt.Printf("📋 Pod Labels:\n")
		var sortedLabels []string
		for key, value := range pod.Labels {
			sortedLabels = append(sortedLabels, fmt.Sprintf("%s=%s", key, value))
		}
		sort.Strings(sortedLabels)

		// Limit labels display like environment variables
		limit := 10
		for i, label := range sortedLabels {
			if i >= limit {
				fmt.Printf("    ... and %d more\n", len(sortedLabels)-limit)
				break
			}
			fmt.Printf("    • %s\n", label)
		}
		fmt.Println()
	}

	// Print annotations
	if len(pod.Annotations) > 0 {
		fmt.Printf("📝 Pod Annotations:\n")
		var sortedAnnotations []string
		for key, value := range pod.Annotations {
			// Truncate very long annotation values for readability
			if len(value) > 100 {
				value = value[:97] + "..."
			}
			sortedAnnotations = append(sortedAnnotations, fmt.Sprintf("%s=%s", key, value))
		}
		sort.Strings(sortedAnnotations)

		// Limit annotations display
		limit := 10
		for i, annotation := range sortedAnnotations {
			if i >= limit {
				fmt.Printf("    ... and %d more\n", len(sortedAnnotations)-limit)
				break
			}
			fmt.Printf("    • %s\n", annotation)
		}
		fmt.Println()
	}
}

// printPodConditions prints pod conditions, especially for pending or problematic pods
func (f *Formatter) printPodConditions(pod types.PodInfo) {
	if len(pod.Conditions) == 0 {
		return
	}

	// Always show conditions for pending pods or if any condition is False
	isPending := pod.Status == "Pending"
	hasFailedConditions := false

	for _, condition := range pod.Conditions {
		if condition.Status == "False" {
			hasFailedConditions = true
			break
		}
	}

	if !isPending && !hasFailedConditions {
		return
	}

	fmt.Printf("🏷️  Conditions:\n")
	fmt.Printf("  %-17s %-7s\n", "Type", "Status")
	for _, condition := range pod.Conditions {
		// Highlight failed conditions in red
		statusDisplay := condition.Status
		if condition.Status == "False" {
			statusDisplay = color.New(color.FgRed).Sprint(condition.Status)
		} else if condition.Status == "True" {
			statusDisplay = color.New(color.FgGreen).Sprint(condition.Status)
		}

		fmt.Printf("  %-17s %s", condition.Type, statusDisplay)

		// Show reason for False conditions
		if condition.Status == "False" && condition.Reason != "" {
			fmt.Printf(" (%s)", condition.Reason)
		}
		fmt.Println()
	}
	fmt.Println()
}

// wrapSchedulingMessage formats long FailedScheduling messages for better readability
func (f *Formatter) wrapSchedulingMessage(message string) string {
	// Try to break on common separators in scheduling messages
	separators := []string{", ", ". preemption:", ": ", " preemption:"}

	for _, sep := range separators {
		if strings.Contains(message, sep) {
			parts := strings.Split(message, sep)
			if len(parts) > 1 {
				// If we found a good break point, format nicely
				result := parts[0]
				for i := 1; i < len(parts); i++ {
					result += sep + "\n      " + strings.TrimSpace(parts[i])
				}
				return result
			}
		}
	}

	// If no good separator found, just return original
	return message
}

// getPodStatusColor returns appropriate color for pod status
func (f *Formatter) getPodStatusColor(status string) *color.Color {
	if f.options.NoColor {
		return color.New()
	}

	switch status {
	case "Running":
		return color.New(color.FgGreen, color.Bold)
	case "Pending":
		return color.New(color.FgYellow, color.Bold)
	case "Succeeded":
		return color.New(color.FgCyan, color.Bold)
	case "Failed":
		return color.New(color.FgRed, color.Bold)
	case "Terminating":
		return color.New(color.FgMagenta, color.Bold)
	default:
		return color.New(color.FgWhite)
	}
}

// printNetworkInfo prints network information for a pod
func (f *Formatter) printNetworkInfo(pod types.PodInfo) {
	networkType := "Pod Network"
	if pod.Network.HostNetwork {
		networkType = "Host Network"
	}

	// Get primary IP (first PodIP or fallback to PodIP field)
	primaryIP := "-"
	if len(pod.Network.PodIPs) > 0 {
		primaryIP = pod.Network.PodIPs[0]
	} else if pod.Network.PodIP != "" {
		primaryIP = pod.Network.PodIP
	}

	// Format network information
	networkInfo := fmt.Sprintf("🌐 NETWORK: %s   IP: %s", networkType, primaryIP)

	// Add additional IPs if there are multiple (dual-stack)
	if len(pod.Network.PodIPs) > 1 {
		additionalIPs := strings.Join(pod.Network.PodIPs[1:], ", ")
		networkInfo += fmt.Sprintf("   ADDITIONAL IPs: %s", additionalIPs)
	}

	// Add host IP if different from pod IP
	if pod.Network.HostIP != "" && pod.Network.HostIP != primaryIP {
		networkInfo += fmt.Sprintf("   HOST IP: %s", pod.Network.HostIP)
	}

	fmt.Printf("%s\n", networkInfo)
}

// calculateAverageValue calculates the average of resource values
func (f *Formatter) calculateAverageValue(values []string) string {
	if len(values) == 0 {
		return "-"
	}

	// For now, just return the first value as a simple average
	// In a more sophisticated implementation, we would parse the values
	// and calculate the actual average, but for display purposes,
	// showing a representative value is sufficient
	return values[0]
}

// calculatePercentileValue calculates the percentile value from a slice of resource values
func (f *Formatter) calculatePercentileValue(values []string, percentile float64) string {
	if len(values) == 0 {
		return "-"
	}

	// Sort the values to calculate percentile
	sortedValues := make([]string, len(values))
	copy(sortedValues, values)
	sort.Strings(sortedValues)

	// Calculate the index for the percentile
	n := len(sortedValues)
	index := int(float64(n) * percentile)

	// Ensure index is within bounds
	if index >= n {
		index = n - 1
	}

	return sortedValues[index]
}

// filterContainers filters containers based on the container name option
func (f *Formatter) filterContainers(containers []types.ContainerInfo) []types.ContainerInfo {
	if f.options.ContainerName == "" {
		return containers
	}

	var filtered []types.ContainerInfo
	for _, container := range containers {
		if container.Name == f.options.ContainerName {
			filtered = append(filtered, container)
		}
	}
	return filtered
}

// shouldShowContainer checks if a container should be shown based on the filter
func (f *Formatter) shouldShowContainer(containerName string) bool {
	if f.options.ContainerName == "" {
		return true
	}
	return containerName == f.options.ContainerName
}
