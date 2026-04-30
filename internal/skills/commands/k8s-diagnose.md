# /k8s-diagnose — Diagnose Kubernetes namespace health

Diagnose a Kubernetes namespace for pod failures, scheduling issues, node pressure, and Helm release problems.

## Usage

```
/k8s-diagnose [namespace]
```

If `$ARGUMENTS` is empty, determine the active namespace by running:

```bash
klens get contexts -o json
```

and using the entry where `"current": true`. Default to `default` if none found.

## Steps

Run the following queries (you may run them in parallel with `&` in a shell, but process results before reporting):

```bash
klens get pods -n <namespace> -o json
klens get events -n <namespace> --since 1h -o json
klens get deployments -n <namespace> -o json
klens get nodes -o json
klens get helmreleases -n <namespace> -o json
```

Note: `klens get helmreleases` exits 0 with `[]` if FluxCD is not installed — skip the Helm section in that case.

## Analysis Checklist

For each finding, classify as **CRITICAL**, **WARNING**, or **INFO**.

### Pod failures (from pods JSON)
- `status.containerStatuses[].state.waiting.reason` in `{CrashLoopBackOff, ImagePullBackOff, ErrImagePull}` → **CRITICAL**
- `status.containerStatuses[].lastState.terminated.reason == "OOMKilled"` → **CRITICAL**
- Pod phase `Failed` → **CRITICAL**
- Pod phase `Pending` for more than a short time (check `creationTimestamp` age) → **WARNING**

### Deployment availability (from deployments JSON)
- `status.readyReplicas < spec.replicas` (or `spec.replicas` is null/absent and ready < 1) → **CRITICAL** if gap ≥ half, **WARNING** otherwise
- `status.availableReplicas == 0` and `spec.replicas > 0` → **CRITICAL**

### Node health (from nodes JSON)
- `status.conditions[?(@.type=="Ready")].status != "True"` → **CRITICAL** (node not ready)
- `status.conditions[?(@.type=="MemoryPressure")].status == "True"` → **WARNING**
- `status.conditions[?(@.type=="DiskPressure")].status == "True"` → **WARNING**
- `status.conditions[?(@.type=="PIDPressure")].status == "True"` → **WARNING**
- `spec.unschedulable == true` (node cordoned) → **INFO**
- Taints containing `node.kubernetes.io/not-ready` or `node.kubernetes.io/unreachable` → **CRITICAL**

### Warning event storms (from events JSON)
- More than 5 `type == "Warning"` events for the same `involvedObject.name` within the last hour → **WARNING**
- Any event with `reason` in `{BackOff, FailedMount, FailedScheduling, Killing, NodeNotReady, OOMKilling}` → **WARNING** (list top 5)

### Helm release problems (from helmreleases JSON, skip if empty)
- `status.conditions[].reason` in `{InstallFailed, UpgradeFailed, ReconciliationFailed}` → **WARNING**

## Output Format

```
NAMESPACE: <namespace>  CONTEXT: <context>

CRITICAL
  <resource-kind>/<name>  <reason>
    → kubectl <suggested-command>

WARNING
  <resource-kind>/<name>  <reason>
    → kubectl <suggested-command>

INFO
  <note>

OK — No issues found in namespace <namespace>   (only if no CRITICAL/WARNING)
```

Group findings by severity. For each finding include exactly one suggested `kubectl` remediation command from the list below:

| Condition | Suggested command |
|-----------|-------------------|
| CrashLoopBackOff deployment | `kubectl rollout restart deployment/<name> -n <namespace>` |
| ImagePullBackOff | `kubectl describe pod/<name> -n <namespace>` (to check image name/credentials) |
| OOMKilled | `kubectl top pod/<name> -n <namespace>` then consider `kubectl edit deployment/<name> -n <namespace>` to raise memory limits |
| Node MemoryPressure | `kubectl cordon <node-name>` then `kubectl drain <node-name> --ignore-daemonsets` |
| Node NotReady | `kubectl describe node/<node-name>` |
| Replica mismatch | `kubectl rollout status deployment/<name> -n <namespace>` |
| Helm InstallFailed/UpgradeFailed | `kubectl describe helmrelease/<name> -n <namespace>` |

**Important:** Only suggest commands — do not run them. The user decides what to apply.
