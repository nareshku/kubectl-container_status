package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/nareshku/kubectl-container-status/pkg/analyzer"
	"github.com/nareshku/kubectl-container-status/pkg/collector"
	"github.com/nareshku/kubectl-container-status/pkg/output"
	"github.com/nareshku/kubectl-container-status/pkg/resolver"
	"github.com/nareshku/kubectl-container-status/pkg/types"
)

// NewContainerStatusCommand creates the root command
func NewContainerStatusCommand() *cobra.Command {
	options := &types.Options{
		Namespace:    "",
		OutputFormat: "table",
		SortBy:       "name",
	}

	cmd := &cobra.Command{
		Use:   "container-status [resource-name] [flags]",
		Short: "Display container status information for Kubernetes pods and workloads",
		Long: `Display container status information for Kubernetes pods and workloads.

This plugin provides a clean, human-friendly view of container-level status and
diagnostics within Kubernetes Pods and their owning workloads (e.g., Deployments,
StatefulSets, Jobs).

Examples:
  # Auto-detection (plugin determines resource type)
  kubectl container-status web-backend

  # Explicit resource type
  kubectl container-status deployment/web-backend
  kubectl container-status pod/mypod-xyz

  # Using flags
  kubectl container-status --deployment web-backend
  kubectl container-status --selector app=web,tier=backend

  # Show only problematic containers and pods (restarts, failures, terminating, etc.)
  kubectl container-status --problematic
  
  # Show recent Kubernetes events (last 1 hour)
  kubectl container-status --events pod/mypod-xyz`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				// Parse resource name/type from argument
				if strings.Contains(args[0], "/") {
					parts := strings.SplitN(args[0], "/", 2)
					options.ResourceType = parts[0]
					options.ResourceName = parts[1]
				} else {
					options.ResourceName = args[0]
				}
			}

			return runContainerStatus(options)
		},
	}

	// Add flags
	cmd.Flags().StringVar(&options.Deployment, "deployment", "", "Show container status for all pods in the given Deployment")
	cmd.Flags().StringVar(&options.StatefulSet, "statefulset", "", "Show container status for all pods in the given StatefulSet")
	cmd.Flags().StringVar(&options.Job, "job", "", "Show container status for all pods in the given Job")
	cmd.Flags().StringVar(&options.DaemonSet, "daemonset", "", "Show container status for all pods in the given DaemonSet")
	cmd.Flags().StringVarP(&options.Selector, "selector", "l", "", "Label selector to fetch and group matching pods")
	cmd.Flags().StringVarP(&options.Namespace, "namespace", "n", "", "Target namespace (defaults to current context)")
	cmd.Flags().StringVar(&options.Context, "context", "", "The name of the kubeconfig context to use")
	cmd.Flags().BoolVar(&options.AllNamespaces, "all-namespaces", false, "Show containers across all namespaces")
	cmd.Flags().BoolVar(&options.Brief, "brief", false, "Print just the summary table (no per-container details)")
	cmd.Flags().StringVar(&options.OutputFormat, "output", "table", "Output format: table, json, yaml")
	cmd.Flags().BoolVar(&options.NoColor, "no-color", false, "Disable colored output")
	cmd.Flags().BoolVar(&options.Problematic, "problematic", false, "Show only problematic containers and pods (restarts, failures, terminating, etc.)")
	cmd.Flags().StringVar(&options.SortBy, "sort", "name", "Sort by: name, restarts, cpu, memory, age")
	cmd.Flags().BoolVar(&options.ShowEnv, "env", false, "Show key environment variables")
	cmd.Flags().BoolVar(&options.ShowEvents, "events", false, "Show recent Kubernetes events related to the pods")
	cmd.Flags().BoolVar(&options.ShowLogs, "logs", false, "Show last 10 lines of container logs (Pod resources only)")

	// Mark some flags as mutually exclusive
	cmd.MarkFlagsMutuallyExclusive("deployment", "statefulset", "job", "daemonset", "selector")
	cmd.MarkFlagsMutuallyExclusive("namespace", "all-namespaces")

	return cmd
}

func runContainerStatus(options *types.Options) error {
	// Determine which resource flag was set
	if options.Deployment != "" {
		options.ResourceType = "deployment"
		options.ResourceName = options.Deployment
	} else if options.StatefulSet != "" {
		options.ResourceType = "statefulset"
		options.ResourceName = options.StatefulSet
	} else if options.Job != "" {
		options.ResourceType = "job"
		options.ResourceName = options.Job
	} else if options.DaemonSet != "" {
		options.ResourceType = "daemonset"
		options.ResourceName = options.DaemonSet
	}

	// Enable extended information by default (previously behind --wide flag)
	options.ShowEvents = true
	options.ShowEnv = true
	options.Wide = true // Set this internally for existing logic compatibility

	// Initialize Kubernetes clients
	configOverrides := &clientcmd.ConfigOverrides{}
	if options.Context != "" {
		configOverrides.CurrentContext = options.Context
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		configOverrides,
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	metricsClient, err := metricsv1beta1.NewForConfig(config)
	if err != nil {
		// Metrics client is optional, continue without it
		fmt.Fprintf(os.Stderr, "Warning: Could not create metrics client: %v\n", err)
		metricsClient = nil
	}

	// Set default namespace if not specified
	if options.Namespace == "" && !options.AllNamespaces {
		namespace, _, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			configOverrides,
		).Namespace()
		if err != nil {
			return fmt.Errorf("failed to get current namespace: %w", err)
		}
		options.Namespace = namespace
	}

	// Initialize components
	resolver := resolver.New(clientset)
	collector := collector.New(clientset, metricsClient)
	analyzer := analyzer.New()
	formatter := output.New(options)

	ctx := context.Background()

	// Single execution mode
	workloads, err := resolver.Resolve(ctx, options)
	if err != nil {
		return fmt.Errorf("failed to resolve resources: %w", err)
	}

	if len(workloads) == 0 {
		return fmt.Errorf("no resources found")
	}

	// Collect data for all workloads
	for i, workload := range workloads {
		// Set optimization flags based on workload type
		// Single pod view gets detailed data, workload views get optimized data
		isSinglePod := workload.Kind == "Pod"
		options.SinglePodView = isSinglePod

		// Restrict --logs to only work with Pod resources
		if options.ShowLogs && !isSinglePod {
			fmt.Fprintf(os.Stderr, "Warning: --logs flag is only supported for individual Pods, ignoring for %s '%s'\n", 
				workload.Kind, workload.Name)
			options.ShowLogs = false
		}

		// Always collect resource usage now that we have efficient bulk collection
		options.ShowResourceUsage = true

		pods, err := collector.CollectPods(ctx, workload, options)
		if err != nil {
			return fmt.Errorf("failed to collect pod data: %w", err)
		}
		workloads[i].Pods = pods

		// Analyze health for each pod
		for j, pod := range workloads[i].Pods {
			workloads[i].Pods[j].Health = analyzer.AnalyzePodHealth(pod)
		}

		// Analyze overall workload health
		workloads[i].Health = analyzer.AnalyzeWorkloadHealth(workloads[i])
	}

	// Filter problems if requested
	if options.Problematic {
		workloads = filterProblematicWorkloads(workloads)
	}

	// Output results
	return formatter.Output(workloads)
}

// filterProblematicWorkloads filters workloads to only include those with problems
func filterProblematicWorkloads(workloads []types.WorkloadInfo) []types.WorkloadInfo {
	var filtered []types.WorkloadInfo

	for _, workload := range workloads {
		hasProblems := false
		var problematicPods []types.PodInfo

		for _, pod := range workload.Pods {
			podHasProblems := false

			// Check if pod itself has problems (pod-level issues)
			if isPodProblematic(pod) {
				podHasProblems = true
			}

			// Check if pod has problematic containers
			if !podHasProblems {
				for _, container := range append(pod.InitContainers, pod.Containers...) {
					if isContainerProblematic(container) {
						podHasProblems = true
						break
					}
				}
			}

			if podHasProblems {
				problematicPods = append(problematicPods, pod)
				hasProblems = true
			}
		}

		if hasProblems {
			workload.Pods = problematicPods
			filtered = append(filtered, workload)
		}
	}

	return filtered
}

// isContainerProblematic checks if a container has problems
func isContainerProblematic(container types.ContainerInfo) bool {
	// Non-zero exit codes
	if container.ExitCode != nil && *container.ExitCode != 0 {
		return true
	}

	// Recent restarts
	if container.RestartCount > 0 {
		return true
	}

	// Bad states
	if container.Status == "CrashLoopBackOff" ||
		container.Status == "Error" ||
		(container.Status == "Terminated" && container.Type != "init") {
		return true
	}

	// Failed probes
	if !container.Probes.Liveness.Passing && container.Probes.Liveness.Configured {
		return true
	}
	if !container.Probes.Readiness.Passing && container.Probes.Readiness.Configured {
		return true
	}

	// High resource usage
	if container.Resources.MemPercentage > 90 {
		return true
	}

	// OOMKilled
	if strings.Contains(container.TerminationReason, "OOMKilled") {
		return true
	}

	return false
}

// isPodProblematic checks if a pod has pod-level problems
func isPodProblematic(pod types.PodInfo) bool {
	// Pods stuck in problematic states
	if pod.Status == "Terminating" ||
		pod.Status == "Failed" ||
		pod.Status == "Unknown" ||
		pod.Status == "Pending" {
		return true
	}

	return false
}
