package types

import (
	"time"
)

// ContainerInfo represents the container status information
type ContainerInfo struct {
	Name              string
	Type              string // "init", "ephemeral", or "standard"
	Status            string
	Ready             bool
	RestartCount      int32
	LastState         string
	LastStateReason   string
	ExitCode          *int32
	StartedAt         *time.Time
	FinishedAt        *time.Time
	LastRestartTime   *time.Time
	Image             string
	Command           []string
	Args              []string
	Resources         ResourceInfo
	Probes            ProbeInfo
	Volumes           []VolumeInfo
	Environment       []EnvVar
	TerminationReason string
	Logs              []string // Container logs (recent lines)
}

// ResourceInfo represents resource usage and limits
type ResourceInfo struct {
	CPURequest    string
	CPULimit      string
	CPUUsage      string
	CPUPercentage float64
	MemRequest    string
	MemLimit      string
	MemUsage      string
	MemPercentage float64
}

// ProbeInfo represents probe configuration and status
type ProbeInfo struct {
	Liveness  ProbeDetails
	Readiness ProbeDetails
	Startup   ProbeDetails
}

// ProbeDetails represents individual probe details
type ProbeDetails struct {
	Configured   bool
	Type         string // HTTP, TCP, Exec
	Path         string
	Port         string
	Passing      bool
	FailureCount int32
	LastError    string
}

// VolumeInfo represents volume mount information
type VolumeInfo struct {
	Name       string
	MountPath  string
	VolumeType string
	Details    string
}

// EnvVar represents environment variable
type EnvVar struct {
	Name   string
	Value  string
	Masked bool
}

// HealthStatus represents the overall health status
type HealthStatus struct {
	Level  string // "Healthy", "Degraded", "Critical"
	Reason string
	Score  int // 0-100
}

// PodInfo represents pod information with container details
type PodInfo struct {
	Name           string
	Namespace      string
	NodeName       string
	ServiceAccount string // Service account used by the pod
	Age            time.Duration
	Status         string
	Health         HealthStatus
	Containers     []ContainerInfo
	InitContainers []ContainerInfo
	Events         []EventInfo
	Metrics        *PodMetrics
	Labels         map[string]string // Pod labels
	Annotations    map[string]string // Pod annotations
	Conditions     []PodCondition    // Pod conditions (PodScheduled, etc.)
	Network        NetworkInfo       // Network information
}

// NetworkInfo represents pod network information
type NetworkInfo struct {
	HostNetwork bool     // Whether pod uses host network
	PodIP       string   // Pod IP address
	HostIP      string   // Host IP address
	PodIPs      []string // Pod IP addresses (for dual-stack)
}

// EventInfo represents kubernetes events
type EventInfo struct {
	Time    time.Time
	Type    string
	Reason  string
	Message string
	PodName string // Track which pod this event belongs to
}

// PodMetrics represents pod-level metrics
type PodMetrics struct {
	CPUUsage    string
	MemoryUsage string
	Containers  map[string]ContainerMetrics
}

// ContainerMetrics represents container-level metrics
type ContainerMetrics struct {
	CPUUsage    string
	MemoryUsage string
}

// WorkloadInfo represents workload information
type WorkloadInfo struct {
	Name      string
	Kind      string
	Namespace string
	Replicas  string
	Labels    map[string]string
	Selector  map[string]string
	Pods      []PodInfo
	Health    HealthStatus
}

// Options represents command-line flags and options
type Options struct {
	ResourceName      string
	ResourceType      string
	Namespace         string
	Context           string // Kubernetes context to use
	AllNamespaces     bool
	Wide              bool
	Brief             bool
	OutputFormat      string // json, yaml, table
	NoColor           bool
	Problematic       bool
	SortBy            string
	ShowEnv           bool
	ShowEvents        bool
	ShowLogs          bool // Show recent container logs
	ShowResourceUsage bool // Show detailed resource usage (CPU/Memory percentages)
	SinglePodView     bool // Whether this is a single pod view (vs workload view)
	Selector          string

	// Resource-specific flags
	Deployment  string
	StatefulSet string
	Job         string
	DaemonSet   string

	// Container filter
	ContainerName string // Filter to show only specific container
}

// ContainerStatusType represents container status types
type ContainerStatusType string

const (
	ContainerStatusRunning    ContainerStatusType = "Running"
	ContainerStatusWaiting    ContainerStatusType = "Waiting"
	ContainerStatusTerminated ContainerStatusType = "Terminated"
	ContainerStatusCompleted  ContainerStatusType = "Completed"
	ContainerStatusUnknown    ContainerStatusType = "Unknown"
)

// HealthLevel represents health status levels
type HealthLevel string

const (
	HealthLevelHealthy  HealthLevel = "Healthy"
	HealthLevelDegraded HealthLevel = "Degraded"
	HealthLevelCritical HealthLevel = "Critical"
)

// ContainerType represents container types
type ContainerType string

const (
	ContainerTypeInit      ContainerType = "init"
	ContainerTypeStandard  ContainerType = "standard"
	ContainerTypeEphemeral ContainerType = "ephemeral"
)

// PodCondition represents pod condition information
type PodCondition struct {
	Type    string // PodScheduled, Initialized, Ready, ContainersReady
	Status  string // True, False, Unknown
	Reason  string
	Message string
}

// SortType represents sort options
type SortType string

const (
	SortByName     SortType = "name"
	SortByRestarts SortType = "restarts"
	SortByCPU      SortType = "cpu"
	SortByMemory   SortType = "memory"
	SortByAge      SortType = "age"
)
