# kubectl container-status

A kubectl plugin that provides a **clean, human-friendly view** of container-level status and diagnostics within Kubernetes Pods and their owning workloads (e.g., Deployments, StatefulSets, Jobs).

## Features

- 🔍 **Smart Resource Detection**: Auto-detect resource types or use explicit specification
- 🏥 **Health Scoring**: Intelligent health analysis with visual indicators
- 📊 **Resource Usage**: Progress bars for CPU and memory usage
- 🔄 **Probe Status**: Display liveness, readiness, and startup probe status
- 📁 **Volume Information**: Show mounted volumes and their types (with `--wide`)
- 🌈 **Colored Output**: Beautiful, colored terminal output with emoji indicators
- 📝 **Multiple Formats**: Table, JSON, and YAML output formats
- 🔍 **Problematic Container Detection**: Filter to show only containers and pods with issues (restarts, failures, terminating, etc.)
- 🎯 **Flexible Targeting**: Support for Deployments, StatefulSets, DaemonSets, Jobs, and Pods

## Installation

### Build from Source

```bash
git clone https://github.com/nareshku/kubectl-container-status
cd kubectl-container-status
make install
```

### Verify Installation

```bash
kubectl container-status --help
```

### Uninstall

```bash
make uninstall
```

## Usage

### Basic Examples

```bash
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

# Brief mode (summary table only)
kubectl container-status --deployment coredns --brief

# Wide mode with volume information
kubectl container-status --deployment coredns --wide

# JSON output
kubectl container-status deployment/coredns --output json
```

### Command Line Flags

| Flag                | Description                                                         |
| ------------------- | ------------------------------------------------------------------- |
| `--deployment`      | Show container status for all pods in the given Deployment          |
| `--statefulset`     | Show container status for all pods in the given StatefulSet         |
| `--job`             | Show container status for all pods in the given Job                 |
| `--daemonset`       | Show container status for all pods in the given DaemonSet           |
| `-l`, `--selector`  | Label selector to fetch and group matching pods                     |
| `-n`, `--namespace` | Target namespace (defaults to current context)                      |
| `--all-namespaces`  | Show containers across all namespaces                               |
| `--wide`            | Show extended info: volumes, env vars, detailed probes              |
| `--brief`           | Print just the summary table (no per-container details)             |
| `--output`          | Output format: table, json, yaml                                   |
| `--no-color`        | Disable colored output                                              |
| `--problematic`     | Show only problematic containers and pods (restarts, failures, terminating, etc.) |
| `--sort`            | Sort by: name, restarts, cpu, memory, age                          |
| `--env`             | Show key environment variables                                      |

## Output Examples

### Deployment View
```
DEPLOYMENT: coredns   REPLICAS: 2/2   NAMESPACE: kube-system
🟢 HEALTH: Healthy (all pods running, no recent restarts)

SUMMARY:
  • 2 Pods matched
  • 2 Running, 0 Warning, 0 Failed
  • Containers: coredns
  • Total Restarts: 4 (last hour)

POD: coredns-76f75df574-66d7q   NODE: kind-control-plane   AGE: 121d
🟢 HEALTH: Healthy (all containers running normally)

┌───────────┬────────────┬──────────┬────────────┬───────────┐
│ CONTAINER │   STATUS   │ RESTARTS │ LAST STATE │ EXIT CODE │
├───────────┼────────────┼──────────┼────────────┼───────────┤
│ coredns   │ 🟢 Running │        2 │ Terminated │ -         │
└───────────┴────────────┴──────────┴────────────┴───────────┘

⚙️  Container: coredns
  • Status:      🟢 Running (started 3d ago)
  • Image:       registry.k8s.io/coredns/coredns:v1.11.1
  • Resources:   CPU: ░░░░░░░░░░ 0% (0m/0)
                 Mem: ░░░░░░░░░░ 0% (0Mi/170Mi)
  • Liveness:    ✅ HTTP /health on port 8080 (passing)
  • Readiness:   ✅ HTTP /ready on port 8181 (passing)
```

### Brief Mode
```
DEPLOYMENT: coredns   REPLICAS: 2/2   NAMESPACE: kube-system
🟢 HEALTH: Healthy (all pods running, no recent restarts)

┌──────────────────────────┬────────────┬───────┬──────────┬──────┐
│           POD            │   STATUS   │ READY │ RESTARTS │ AGE  │
├──────────────────────────┼────────────┼───────┼──────────┼──────┤
│ coredns-76f75df574-66d7q │ 🟢 Healthy │ 1/1   │        2 │ 121d │
│ coredns-76f75df574-prcth │ 🟢 Healthy │ 1/1   │        2 │ 121d │
└──────────────────────────┴────────────┴───────┴──────────┴──────┘
```

## Health Status Indicators

| Status | Icon | Criteria |
|--------|------|----------|
| Healthy | 🟢 | All containers running, no restarts in 1h, all probes passing |
| Degraded | 🟡 | Some containers restarting or probe failures |
| Critical | 🔴 | Containers in CrashLoopBackOff or multiple failures |

## Container Status Icons

| Status | Icon | Description |
|--------|------|-------------|
| Running | 🟢 | Container is running normally |
| Completed | ✅ | Container completed successfully (init containers) |
| CrashLoopBackOff/Error | 🔴 | Container is failing |
| Waiting | 🟡 | Container waiting to start |
| Terminated | 🔴 | Container terminated unexpectedly |

## Resource Usage Visualization

Resource usage is displayed with 10-segment progress bars:
- `▓` = Used capacity
- `░` = Available capacity

```
CPU: ▓▓▓░░░░░░░ 30% (60m/200m)
Mem: ▓▓▓▓▓▓▓▓░░ 80% (1Gi/1.25Gi) ⚠️
```

## Problematic Container Detection

The `--problematic` flag filters the output to show only containers and pods with issues. This is useful for troubleshooting and quickly identifying pods that need attention.

### What Makes a Pod "Problematic"?

**Pod-Level Issues:**
- **Terminating**: Pods stuck in terminating state (with deletionTimestamp set)
- **Failed**: Pods that have failed to run
- **Unknown**: Pods in unknown state (usually node communication issues)  
- **Pending**: Pods stuck in pending state (scheduling issues)

**Container-Level Issues:**
- **Restarts**: Any container with restart count > 0
- **Non-zero Exit Codes**: Containers that have crashed or terminated abnormally
- **Bad States**: 
  - `CrashLoopBackOff` - Container repeatedly crashing
  - `Error` - Container in error state
  - `Terminated` - Regular containers that have terminated unexpectedly
- **Failed Probes**: 
  - Liveness probe failures (container will be restarted)
  - Readiness probe failures (traffic won't be routed)
- **Resource Issues**:
  - Memory usage > 90% (approaching limits)
  - `OOMKilled` termination (out of memory)

### Examples

```bash
# Show all problematic pods across a deployment
kubectl container-status --deployment webapp --problematic

# Find problematic pods with brief output for quick overview
kubectl container-status --problematic --brief

# Check specific workload for issues
kubectl container-status ds/fluent-bit --problematic
```

### Use Cases

- **Troubleshooting**: Quickly identify pods with issues
- **Health Monitoring**: Filter out healthy pods to focus on problems
- **Restart Investigation**: Find containers that have been restarting
- **Resource Issues**: Identify pods with memory/CPU problems
- **Stuck Pods**: Find pods in terminating or pending states

## Resource Auto-Detection

The plugin automatically detects resource types in this order:
1. Check if input matches `type/name` pattern
2. Try to find as Pod first
3. Try Deployment, StatefulSet, DaemonSet, Job, ReplicaSet in order
4. If multiple matches found, show error with suggestions

## Development

### Prerequisites
- Go 1.21+
- Make (build tool)
- Access to a Kubernetes cluster

### Building
```bash
# Build for current platform
make build

# Build for all platforms
make build-all
```

### Running Tests
```bash
# Run all tests
make test

# Run tests with coverage report
make test-coverage
```

### Development Commands
```bash
# Format code
make fmt

# Run linter
make vet

# Clean build artifacts
make clean

# Update dependencies
make mod-tidy

# Quick dev test (build and verify basic functionality)
make dev-test

# See all available commands
make help
```


## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
