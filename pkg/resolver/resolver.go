package resolver

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/nareshku/kubectl-container-status/pkg/types"
)

// Resolver handles resource resolution and auto-detection
type Resolver struct {
	clientset kubernetes.Interface
}

// New creates a new resolver instance
func New(clientset kubernetes.Interface) *Resolver {
	return &Resolver{
		clientset: clientset,
	}
}

// Resolve resolves the resource specification to workload information
func (r *Resolver) Resolve(ctx context.Context, options *types.Options) ([]types.WorkloadInfo, error) {
	if options.Selector != "" {
		return r.resolveBySelector(ctx, options)
	}

	if options.ResourceName == "" {
		return nil, fmt.Errorf("resource name is required")
	}

	if options.ResourceType == "" {
		// Auto-detect resource type
		return r.autoDetectAndResolve(ctx, options)
	}

	// Explicit resource type
	return r.resolveByType(ctx, options)
}

// resolveBySelector resolves resources using label selector
func (r *Resolver) resolveBySelector(ctx context.Context, options *types.Options) ([]types.WorkloadInfo, error) {
	selector, err := labels.Parse(options.Selector)
	if err != nil {
		return nil, fmt.Errorf("invalid selector: %w", err)
	}

	namespace := options.Namespace
	if options.AllNamespaces {
		namespace = ""
	}

	// Get pods matching the selector
	pods, err := r.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found matching selector %s", options.Selector)
	}

	// Group pods by owner
	workloadMap := make(map[string]*types.WorkloadInfo)

	for _, pod := range pods.Items {
		workload := r.getWorkloadFromPod(&pod)
		if workload == nil {
			// Standalone pod
			key := fmt.Sprintf("pod/%s/%s", pod.Namespace, pod.Name)
			workloadMap[key] = &types.WorkloadInfo{
				Name:      pod.Name,
				Kind:      "Pod",
				Namespace: pod.Namespace,
				Replicas:  "1/1",
				Labels:    pod.Labels,
			}
		} else {
			key := fmt.Sprintf("%s/%s/%s", workload.Kind, workload.Namespace, workload.Name)
			if existing, exists := workloadMap[key]; exists {
				workloadMap[key] = existing
			} else {
				workloadMap[key] = workload
			}
		}
	}

	// Convert map to slice
	var workloads []types.WorkloadInfo
	for _, workload := range workloadMap {
		workloads = append(workloads, *workload)
	}

	return workloads, nil
}

// autoDetectAndResolve attempts to auto-detect the resource type
func (r *Resolver) autoDetectAndResolve(ctx context.Context, options *types.Options) ([]types.WorkloadInfo, error) {
	resourceName := options.ResourceName
	namespace := options.Namespace

	// Try Pod first - when user specifies a pod name directly, show only that pod
	if pod, err := r.clientset.CoreV1().Pods(namespace).Get(ctx, resourceName, metav1.GetOptions{}); err == nil {
		// For direct pod specification, always treat as standalone pod
		workload := &types.WorkloadInfo{
			Name:      pod.Name,
			Kind:      "Pod",
			Namespace: pod.Namespace,
			Replicas:  "1/1",
			Labels:    pod.Labels,
		}
		return []types.WorkloadInfo{*workload}, nil
	}

	// Try Deployment
	if deployment, err := r.clientset.AppsV1().Deployments(namespace).Get(ctx, resourceName, metav1.GetOptions{}); err == nil {
		workload := &types.WorkloadInfo{
			Name:      deployment.Name,
			Kind:      "Deployment",
			Namespace: deployment.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", deployment.Status.ReadyReplicas, deployment.Status.Replicas),
			Labels:    deployment.Labels,
			Selector:  deployment.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil
	}

	// Try StatefulSet
	if statefulset, err := r.clientset.AppsV1().StatefulSets(namespace).Get(ctx, resourceName, metav1.GetOptions{}); err == nil {
		workload := &types.WorkloadInfo{
			Name:      statefulset.Name,
			Kind:      "StatefulSet",
			Namespace: statefulset.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", statefulset.Status.ReadyReplicas, statefulset.Status.Replicas),
			Labels:    statefulset.Labels,
			Selector:  statefulset.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil
	}

	// Try DaemonSet
	if daemonset, err := r.clientset.AppsV1().DaemonSets(namespace).Get(ctx, resourceName, metav1.GetOptions{}); err == nil {
		workload := &types.WorkloadInfo{
			Name:      daemonset.Name,
			Kind:      "DaemonSet",
			Namespace: daemonset.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", daemonset.Status.NumberReady, daemonset.Status.DesiredNumberScheduled),
			Labels:    daemonset.Labels,
			Selector:  daemonset.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil
	}

	// Try Job
	if job, err := r.clientset.BatchV1().Jobs(namespace).Get(ctx, resourceName, metav1.GetOptions{}); err == nil {
		workload := &types.WorkloadInfo{
			Name:      job.Name,
			Kind:      "Job",
			Namespace: job.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", job.Status.Succeeded, *job.Spec.Completions),
			Labels:    job.Labels,
			Selector:  job.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil
	}

	return nil, fmt.Errorf("resource '%s' not found as Pod, Deployment, StatefulSet, DaemonSet, or Job", resourceName)
}

// resolveByType resolves resource by explicit type
func (r *Resolver) resolveByType(ctx context.Context, options *types.Options) ([]types.WorkloadInfo, error) {
	resourceName := options.ResourceName
	resourceType := strings.ToLower(options.ResourceType)
	namespace := options.Namespace

	switch resourceType {
	case "pod", "pods", "po":
		pod, err := r.clientset.CoreV1().Pods(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get pod: %w", err)
		}
		// For explicit pod specification, always treat as standalone pod
		workload := &types.WorkloadInfo{
			Name:      pod.Name,
			Kind:      "Pod",
			Namespace: pod.Namespace,
			Replicas:  "1/1",
			Labels:    pod.Labels,
		}
		return []types.WorkloadInfo{*workload}, nil

	case "deployment", "deployments", "deploy":
		deployment, err := r.clientset.AppsV1().Deployments(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get deployment: %w", err)
		}
		workload := &types.WorkloadInfo{
			Name:      deployment.Name,
			Kind:      "Deployment",
			Namespace: deployment.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", deployment.Status.ReadyReplicas, deployment.Status.Replicas),
			Labels:    deployment.Labels,
			Selector:  deployment.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil

	case "statefulset", "statefulsets", "sts":
		statefulset, err := r.clientset.AppsV1().StatefulSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get statefulset: %w", err)
		}
		workload := &types.WorkloadInfo{
			Name:      statefulset.Name,
			Kind:      "StatefulSet",
			Namespace: statefulset.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", statefulset.Status.ReadyReplicas, statefulset.Status.Replicas),
			Labels:    statefulset.Labels,
			Selector:  statefulset.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil

	case "daemonset", "daemonsets", "ds":
		daemonset, err := r.clientset.AppsV1().DaemonSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get daemonset: %w", err)
		}
		workload := &types.WorkloadInfo{
			Name:      daemonset.Name,
			Kind:      "DaemonSet",
			Namespace: daemonset.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", daemonset.Status.NumberReady, daemonset.Status.DesiredNumberScheduled),
			Labels:    daemonset.Labels,
			Selector:  daemonset.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil

	case "job", "jobs":
		job, err := r.clientset.BatchV1().Jobs(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get job: %w", err)
		}
		completions := int32(1)
		if job.Spec.Completions != nil {
			completions = *job.Spec.Completions
		}
		workload := &types.WorkloadInfo{
			Name:      job.Name,
			Kind:      "Job",
			Namespace: job.Namespace,
			Replicas:  fmt.Sprintf("%d/%d", job.Status.Succeeded, completions),
			Labels:    job.Labels,
			Selector:  job.Spec.Selector.MatchLabels,
		}
		return []types.WorkloadInfo{*workload}, nil

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// getWorkloadFromPod extracts workload information from a pod's owner references
func (r *Resolver) getWorkloadFromPod(pod *corev1.Pod) *types.WorkloadInfo {
	for _, owner := range pod.OwnerReferences {
		switch owner.Kind {
		case "ReplicaSet":
			// For ReplicaSet, we need to check if it's owned by a Deployment
			if rs, err := r.clientset.AppsV1().ReplicaSets(pod.Namespace).Get(context.Background(), owner.Name, metav1.GetOptions{}); err == nil {
				for _, rsOwner := range rs.OwnerReferences {
					if rsOwner.Kind == "Deployment" {
						return &types.WorkloadInfo{
							Name:      rsOwner.Name,
							Kind:      "Deployment",
							Namespace: pod.Namespace,
							Labels:    pod.Labels,
						}
					}
				}
			}
		case "StatefulSet":
			return &types.WorkloadInfo{
				Name:      owner.Name,
				Kind:      "StatefulSet",
				Namespace: pod.Namespace,
				Labels:    pod.Labels,
			}
		case "DaemonSet":
			return &types.WorkloadInfo{
				Name:      owner.Name,
				Kind:      "DaemonSet",
				Namespace: pod.Namespace,
				Labels:    pod.Labels,
			}
		case "Job":
			return &types.WorkloadInfo{
				Name:      owner.Name,
				Kind:      "Job",
				Namespace: pod.Namespace,
				Labels:    pod.Labels,
			}
		}
	}
	return nil
}
