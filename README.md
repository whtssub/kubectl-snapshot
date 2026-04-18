# kubectl-snapshot

A `kubectl` plugin for capturing point-in-time snapshots of Kubernetes cluster state and diffing them for post-incident analysis.

## What it does

- **Capture** — serialises 24 resource types into a portable JSON bundle
- **Diff** — compares two bundles and shows what was added, removed, or changed
- **Analyze** — inspects a bundle for incident signals and produces a severity-scored report

## Install

### From source (Go 1.22+)

```bash
go install github.com/whtssub/kubectl-snapshot/cmd/kubectl-snapshot@latest
```

### From a release binary

Download the archive for your platform from the [Releases](https://github.com/whtssub/kubectl-snapshot/releases) page, extract it, and put the `kubectl-snapshot` binary somewhere on your `PATH`.

```bash
# Example: macOS arm64
tar -xzf kubectl-snapshot_v0.1.0_darwin_arm64.tar.gz
mv kubectl-snapshot ~/.local/bin/
```

`kubectl` discovers it automatically because the binary is named `kubectl-snapshot`.

## Captured resource types

| Category | Resources |
|----------|-----------|
| Core workloads | pods, nodes, events |
| App workloads | deployments, replicasets, statefulsets, daemonsets, jobs, cronjobs |
| Networking | services, endpoints, ingresses, networkpolicies |
| Storage | persistentvolumeclaims, persistentvolumes |
| Config | configmaps *, secrets * |
| RBAC | serviceaccounts, roles, rolebindings, clusterroles, clusterrolebindings |
| Autoscaling | horizontalpodautoscalers, verticalpodautoscalers † |

\* `.data` and `.binaryData` are **never written to disk** — only metadata is captured.  
† Silently skipped on clusters without the VPA operator.

## Usage

### Capture

```bash
# Full cluster (all namespaces, all resource types)
kubectl snapshot capture -o snap.json

# Single namespace
kubectl snapshot capture -n production -o snap.json

# Specific resource types (short names, plural names, or group/version/resource)
kubectl snapshot capture --resources pods,deploy,pvc -o snap.json
kubectl snapshot capture --resources myapp.io/v1/widgets -o snap.json

# Compressed output (~75% smaller for large clusters)
kubectl snapshot capture --compress gzip -o snap.json.gz
```

### Diff

```bash
kubectl snapshot diff before.json after.json
kubectl snapshot diff before.json after.json --max-items 30
```

```
Snapshot Diff Report
--------------------
Before records: 51
After records:  84
Added:          33
Removed:        0
Changed:        1
Net delta:      +33

## ADDED RESOURCES
- deployments default/api-server
- persistentvolumeclaims default/data-pvc
...
```

### Analyze

```bash
kubectl snapshot analyze snap.json
kubectl snapshot analyze snap.json --severity-threshold medium
kubectl snapshot analyze snap.json --no-resource-mix --no-warning-events
```

```
Snapshot Incident Analysis
-------------------------
Captured at:        2026-04-17 10:00:00 UTC
Cluster context:    kind-prod
Total records:      312
Total restarts:     6
Warning events:     10
Non-normal events:  0

## INCIDENT SCORE
- severity: HIGH
- score: 43 (pods*3 + nodes*4 + workloads*3 + storage*2 + warnings + restarts)

## POD ISSUES
- [CRASHLOOP] sre-lab/api-5d8b9f container=app msg="back-off restarting failed container"
- [OOMKILLED] sre-lab/worker container=main
- sre-lab/batch-job phase=Failed

## WORKLOAD ISSUES
- [DEPLOY] sre-lab/api rollout-failed reason=ProgressDeadlineExceeded
- [STS] sre-lab/postgres ready=1 desired=3
- [HPA] sre-lab/api at-max-replicas current=10 max=10

## STORAGE ISSUES
- [PVC] sre-lab/data-vol phase=Pending
- [PV] pv-archive phase=Released

## NODE ISSUES
- node1 MemoryPressure=True reason=KubeletHasInsufficientMemory

## WARNING EVENTS
- sre-lab/api.1a2b3c reason=BackOff msg="back-off restarting failed container app..."
```

### Version

```bash
kubectl snapshot version
# kubectl-snapshot v0.1.0 (commit: abc1234, built: 2026-04-17)
```

## Supported flags

| Command | Flag | Description |
|---------|------|-------------|
| `capture` | `--output`, `-o` | Output file path (required) |
| `capture` | `--namespace`, `-n` | Limit to one namespace (default: all) |
| `capture` | `--kubeconfig` | Path to kubeconfig file |
| `capture` | `--resources` | Comma-separated list of resource types to capture |
| `capture` | `--compress` | Compress output: `gzip` |
| `diff` | `--max-items` | Max entries per section (default: 15) |
| `analyze` | `--max-items` | Max entries per section (default: 15) |
| `analyze` | `--severity-threshold` | Suppress output below this level: `low`, `medium`, `high` (also tightens per-section item limits) |
| `analyze` | `--no-resource-mix` | Hide resource mix section |
| `analyze` | `--no-warning-events` | Hide warning events section |

## Understanding severity thresholds

The `analyze` command scores a snapshot using:

```
score = pods×3 + nodes×4 + workloads×3 + storage×2 + warnings + restarts
```

Restart count is **capped at 50** before being added to the score, so a pod with 1 000 restarts does not inflate the score to dangerous levels. The raw count is still shown in the header.

| Severity | Score range | What `--severity-threshold` does |
|----------|-------------|----------------------------------|
| LOW | < 15 | No filtering; all sections shown at full `--max-items` |
| MEDIUM | 15 – 39 | Suppresses LOW snapshots; shows up to 50 items per section |
| HIGH | ≥ 40 | Suppresses LOW and MEDIUM snapshots; shows up to 10 items per section |

Job-owned pods are excluded from all analysis — they are meant to run to completion and their exit states are not incident signals.

## Local development

### Prerequisites

- Go 1.22+
- [kind](https://kind.sigs.k8s.io/) (for integration testing)
- Docker Desktop (for kind)

### Build

```bash
make build
make install-plugin   # copies binary to ~/.local/bin
make plugin-check     # verifies kubectl discovers it
```

### Run the SRE fault-lab

Spins up a local Kind cluster and injects real failure scenarios:

```bash
make kind-up
make capture-before
make scenario-all      # OOMKill, CrashLoop, ImagePullBackOff, Pending, DiskPressure
make capture-after
make diff
make analyze
make kind-down
```

Included scenarios (namespace `sre-lab`):

| Scenario | What it demonstrates |
|----------|---------------------|
| `oomkill-demo` | OOMKilled container, `[OOMKILLED]` in analyze output |
| `crashloop-demo` | CrashLoopBackOff, `[CRASHLOOP]` in analyze output |
| `imagepullbackoff-demo` | ErrImagePull / ImagePullBackOff waiting state |
| `pending-unschedulable-demo` | Insufficient CPU/memory, pod stuck Pending |
| `diskpressure-best-effort` | Best-effort DiskPressure trigger on node |

### Run tests

```bash
go test -race ./...
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
