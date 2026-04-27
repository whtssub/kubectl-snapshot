```
  _          _               _   _                                              _           _
 | | ___   _| |__   ___  ___| |_| |   ___ _ __   __ _ _ __  ___| |__   ___ | |_
 | |/ / | | | '_ \ / _ \/ __| __| |  / __| '_ \ / _` | '_ \/ __| '_ \ / _ \| __|
 |   <| |_| | |_) |  __/ (__| |_| |  \__ \ | | | (_| | |_) \__ \ | | | (_) | |_
 |_|\_\\__,_|_.__/ \___|\___|\__|_|  |___/_| |_|\__,_| .__/|___/_| |_|\___/ \__|
                                                       |_|

              📸  point-in-time Kubernetes cluster forensics
```

[![CI](https://github.com/whtssub/kubectl-snapshot/actions/workflows/release.yml/badge.svg)](https://github.com/whtssub/kubectl-snapshot/actions)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/whtssub/kubectl-snapshot)](https://github.com/whtssub/kubectl-snapshot/releases)

A `kubectl` plugin that captures point-in-time snapshots of Kubernetes cluster state and analyzes them for post-incident review. Freeze what your cluster looked like, diff two snapshots to see what changed, or run a scored incident analysis to surface pod failures, node pressure, deployment stalls, and storage problems — all from a single portable JSON file.

---

## What it does

| Command | Description |
|---------|-------------|
| `capture` | Serialises 24 resource types into a portable JSON bundle |
| `diff` | Compares two bundles — shows what was added, removed, or changed |
| `analyze` | Inspects a bundle for incident signals with a severity-scored report |

---

## Install

### From a release binary (recommended)

Download the archive for your platform from the [Releases](https://github.com/whtssub/kubectl-snapshot/releases) page, extract it, and place the binary on your `PATH`.

```bash
# macOS arm64
tar -xzf kubectl-snapshot_v0.2.0_darwin_arm64.tar.gz
mv kubectl-snapshot ~/.local/bin/

# Linux amd64
tar -xzf kubectl-snapshot_v0.2.0_linux_amd64.tar.gz
mv kubectl-snapshot ~/.local/bin/
```

`kubectl` discovers it automatically because the binary is named `kubectl-snapshot`.

### From source (Go 1.22+)

```bash
go install github.com/whtssub/kubectl-snapshot/cmd/kubectl-snapshot@latest
```

---

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

📋 ADDED RESOURCES
─────────────────────────────────
   1. deployments default/api-server
   2. persistentvolumeclaims default/data-pvc
   3. pods default/worker-7d9f
  ... and 30 more

📋 REMOVED RESOURCES
─────────────────────────────────
  ✓ none

📋 CHANGED RESOURCES
─────────────────────────────────
   1. deployments default/frontend
```

### Analyze

```bash
kubectl snapshot analyze snap.json
kubectl snapshot analyze snap.json --severity-threshold medium
kubectl snapshot analyze snap.json --no-resource-mix --no-warning-events
```

```
📸 Snapshot Incident Analysis
═════════════════════════════════
Captured at:        2026-04-17 10:00:00 UTC
Cluster context:    kind-prod
Total records:      312
Total restarts:     6
Warning events:     10
Non-normal events:  0

⚠️ INCIDENT SCORE
- severity: 🔴 HIGH
- score:    43
- formula:  pods×3 + nodes×4 + workloads×3 + storage×2 + warnings + restarts (cap 50)
- thresholds: LOW <15 · MEDIUM 15–39 · HIGH ≥40

📦 RESOURCE MIX
  pods                         184
  events                        72
  deployments                   18
  replicasets                   18

🐳 POD ISSUES
─────────────────────────────────
   1. [CRASHLOOP] sre-lab/api-5d8b9f container=app msg="back-off restarting failed container"
   2. [OOMKILLED] sre-lab/worker container=main
   3. sre-lab/batch phase=Failed

⚙️  WORKLOAD ISSUES
─────────────────────────────────
   1. [DEPLOY] sre-lab/api available=0 desired=3
   2. [DEPLOY] sre-lab/api rollout-stalled reason=ProgressDeadlineExceeded
   3. [STS] sre-lab/postgres ready=1 desired=3
   4. [HPA] sre-lab/api at-max-replicas current=10 max=10
   5. [JOB] sre-lab/etl-pipeline failed reason=BackoffLimitExceeded
   6. [CRONJOB] sre-lab/nightly-report never-succeeded last-schedule=2026-04-17T10:00:00Z

💾 STORAGE ISSUES
─────────────────────────────────
   1. [PVC] sre-lab/data-vol phase=Pending
   2. [PV] pv-archive phase=Released

🖥️  NODE ISSUES
─────────────────────────────────
   1. node1 MemoryPressure=True reason=KubeletHasInsufficientMemory

⚠️  WARNING EVENTS
─────────────────────────────────
   1. sre-lab/api.1a2b3c reason=BackOff msg="back-off restarting failed container app..."
```

> **Color output** is enabled by default. Set `NO_COLOR=1` to disable.


---

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

---

## Understanding severity

The `analyze` command scores a snapshot using:

```
score = pods×3 + nodes×4 + workloads×3 + storage×2 + warnings + restarts
```

Restart count is **capped at 50** before scoring — a pod with 1 000 restarts won't inflate the score to noise. The raw count is always shown in the header.

| Severity | Score | `--severity-threshold` effect |
|----------|-------|-------------------------------|
| 🟢 LOW | < 15 | No filtering; all sections shown at full `--max-items` |
| 🟡 MEDIUM | 15 – 39 | Suppresses LOW results; up to 50 items per section |
| 🔴 HIGH | ≥ 40 | Suppresses LOW + MEDIUM results; up to 10 items per section |

**Job-owned pods** are excluded from pod issue analysis — they run to completion by design. The Jobs and CronJobs themselves are analyzed under **WORKLOAD ISSUES**:

| Signal | Condition |
|--------|-----------|
| `[JOB] <name> suspended` | `spec.suspend: true` |
| `[JOB] <name> failed reason=<r>` | `status.conditions[Failed=True]` |
| `[JOB] <name> failed-attempts=N` | `status.failed > 0`, no `Complete` condition |
| `[CRONJOB] <name> suspended` | `spec.suspend: true` |
| `[CRONJOB] <name> never-succeeded` | scheduled at least once but `lastSuccessfulTime` is absent |

---

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
| `analyze` | `--severity-threshold` | Suppress output below this level: `low`, `medium`, `high` |
| `analyze` | `--no-resource-mix` | Hide resource mix section |
| `analyze` | `--no-warning-events` | Hide warning events section |

---

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

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
