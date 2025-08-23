# kubectl container-status

A kubectl plugin that provides a **clean, human-friendly view** of container-level status and diagnostics within Kubernetes Pods and their owning workloads (e.g., Deployments, StatefulSets, Jobs).

## Features

- 🔍 **Smart Resource Detection**: Auto-detect resource types or use explicit specification
- 🏥 **Health Scoring**: Intelligent health analysis with visual indicators
- 📊 **Resource Usage**: Progress bars for CPU and memory usage with actual values
- 🌐 **Network Information**: Display host vs pod network configuration and IP addresses
- 🎯 **Container Filtering**: Filter to show only specific containers with `-c` flag
- 📝 **Multiple Formats**: Table, JSON, and YAML output formats
- 🔍 **Problematic Container Detection**: Filter to show only containers and pods with issues

## Installation

```bash
git clone https://github.com/nareshku/kubectl-container_status
cd kubectl-container_status
make install
```

## Usage

### Basic Examples

```bash
# Auto-detection (plugin determines resource type)
kubectl container-status coredns-76f75df574-66d7q

# Explicit resource type
kubectl container-status deployment/coredns -n kube-system
kubectl container-status pod/coredns-76f75df574-66d7q -n kube-system

# Using flags
kubectl container-status --deployment coredns -n kube-system
kubectl container-status --daemonset kindnet -n kube-system
kubectl container-status --selector k8s-app=kube-dns -n kube-system

# Filter to show only a specific container
kubectl container-status coredns-76f75df574-66d7q -n kube-system -c coredns
kubectl container-status deployment/coredns -n kube-system -c coredns

# Show only problematic containers
kubectl container-status deploy/coredns --problematic
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
| `--context`         | The name of the kubeconfig context to use                           |
| `--all-namespaces`  | Show containers across all namespaces                               |
| `--output`          | Output format: table, json, yaml                                   |
| `--no-color`        | Disable colored output                                              |
| `--problematic`     | Show only problematic containers and pods (restarts, failures, terminating, etc.) |
| `--sort`            | Sort by: name, restarts, cpu, memory, age                          |
| `--env`             | Show key environment variables                                      |
| `--events`          | Show recent Kubernetes events with enhanced visual indicators       |
| `-c`, `--container` | Show only the specified container                                   |

## Output Examples

### Deployment View
```
────────────────────────────────────────────────────────────
🎯 DEPLOYMENT: coredns   REPLICAS: 2/2   🏷️  NAMESPACE: kube-system   🌐 NETWORK: Pod
┌─ HEALTH STATUS ──────────────────────────────────────┐
│ 🟢 HEALTHY    all pods running normally           (💚)     │
└─────────────────────────────────────────────────────┘

+---------------------------+---------------------------+------------+-------+-----------------+------------+------+
| POD                       | NODE                      | STATUS     | READY | RESTARTS        | IP         | AGE  |
+---------------------------+---------------------------+------------+-------+-----------------+------------+------+
| coredns-76f75df574-66d7q  | kind-control-plane        | 🟢 Healthy |  1/1  | 3 (last 3d ago) | 10.244.0.4 | 162d |
| coredns-76f75df574-prcth  | kind-control-plane        | 🟢 Healthy |  1/1  | 3 (last 3d ago) | 10.244.0.2 | 162d |
+---------------------------+---------------------------+------------+-------+-----------------+------------+------+
```

### Pod View
```
────────────────────────────────────────────────────────────
🎯 POD: coredns-76f75df574-66d7q   CONTAINERS: 1/1   📍 NODE: kind-control-plane   ⏰ AGE: 162d   🏷️  NAMESPACE: kube-system   🔐 SERVICE ACCOUNT: coredns
🌐 NETWORK: Pod Network   IP: 10.244.0.4   HOST IP: 172.18.0.2
┌─ HEALTH STATUS ──────────────────────────────────────┐
│ 🟢 HEALTHY    all pods running normally           (💚)     │
└─────────────────────────────────────────────────────┘

+----------------------+------------+-----------------+----------------------+-----------+
| CONTAINER            | STATUS     | RESTARTS        | LAST STATE           | EXIT CODE |
+----------------------+------------+-----------------+----------------------+-----------+
| coredns              | 🟢 Running | 3 (last 3d ago) | Terminated (Unknown) |    255    |
+----------------------+------------+-----------------+----------------------+-----------+

⚙️  Container: coredns
  • Status:      🟢 Running (started 3d ago)
  • Image:       registry.k8s.io/coredns/coredns:v1.11.1
  • Resources:   CPU: ░░░░░░░░░░ 0% (0m/0m)
                 Mem: ░░░░░░░░░░ 0% (0Mi/170Mi)
  • Liveness:    ✅ HTTP /health on port 8080 (passing)
  • Readiness:   ✅ HTTP /ready on port 8181 (passing)
```

## Health Status Indicators

| Status | Icon | Criteria |
|--------|------|----------|
| Healthy | 🟢 💚 | All containers running, no restarts in 1h, all probes passing |
| Degraded | 🟡 ⚠️ | Some containers restarting or probe failures |
| Critical | 🔴 🚨 | Containers in CrashLoopBackOff or multiple failures |

## Container Filtering

The `-c` or `--container` flag allows you to filter the output to show only a specific container.

### Usage Examples

```bash
# Filter a specific container in a pod
kubectl container-status coredns-76f75df574-66d7q -n kube-system -c coredns

# Filter a specific container across a workload
kubectl container-status deployment/coredns -n kube-system -c coredns

# Filter a daemonset container
kubectl container-status --daemonset kindnet -n kube-system -c kindnet

# Filter with selector
kubectl container-status -l k8s-app=kube-dns -n kube-system -c coredns
```

## Enhanced Resource Usage

Resource usage displays both percentages and actual values:

```
Usage: CPU ▓░░░░░░░ avg:1% (70m) ▓░░░░░░░ p90:1% (70m) ▓░░░░░░░ p99:1% (70m)
       Mem ▓░░░░░░░ avg:1% (14Mi) ▓░░░░░░░ p90:1% (15Mi) ▓░░░░░░░ p99:1% (15Mi)
```

- **📊 Percentages**: CPU and Memory usage as percentages
- **📏 Actual Values**: Real resource consumption (e.g., "70m" CPU, "14Mi" Memory)
- **📈 Percentiles**: Average, P90, and P99 values for workloads with multiple pods
