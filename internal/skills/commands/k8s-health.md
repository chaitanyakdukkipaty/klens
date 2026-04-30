# /k8s-health — Kubernetes namespace stability health report

Generate a PASS/WARN/FAIL health report for a Kubernetes namespace over a configurable time window.

## Usage

```
/k8s-health [namespace] [time-window]
```

Parse `$ARGUMENTS`:
- First token = namespace (default: `default`)
- Second token = time window duration (default: `1h`)

Examples: `/k8s-health default 6h`, `/k8s-health production 30m`, `/k8s-health` (uses default/1h)

## Steps

Run these queries for the given namespace and time window:

```bash
klens get pods -n <namespace> -o json
klens get events -n <namespace> --since <window> -o json
klens get deployments -n <namespace> -o json
```

## Metrics to Compute

From the JSON output, compute these four metrics:

### 1. Pod uptime %
```
(count of pods where phase == "Running" OR phase == "Succeeded") / total pod count * 100
```
If there are 0 pods, uptime = 100% and note "0 pods found".

### 2. Total restarts
```
sum of status.containerStatuses[].restartCount across all pods and all containers
```
Note: restart counts are cumulative from pod creation, not bounded to the time window.

### 3. OOM kill count
```
count of pods where any containerStatuses[].lastState.terminated.reason == "OOMKilled"
```

### 4. Deployment availability
```
count of deployments where status.availableReplicas == spec.replicas (or spec.replicas absent and availableReplicas >= 1)
```
Report as `<available> / <total>`.

### 5. Warning event count
```
count of events where type == "Warning" within the time window
```

## Verdict Rules

| Verdict | Conditions |
|---------|-----------|
| **PASS** | uptime ≥ 95% AND total restarts < 5 AND OOM kills == 0 AND warning events == 0 |
| **WARN** | uptime ≥ 80% AND total restarts < 20 AND OOM kills < 3 AND warning events < 10 |
| **FAIL** | any metric outside WARN thresholds |

## Output Format

```
╔══════════════════════════════════════════════════╗
║  HEALTH REPORT  namespace:<ns>  window:<window>  ║
║  Verdict: PASS / WARN / FAIL                     ║
╚══════════════════════════════════════════════════╝

METRIC              VALUE       THRESHOLD
Pod uptime          99.2%       ≥95% (PASS)
Total restarts      3           <5   (PASS)
OOM kills           0           0    (PASS)
Deployments ready   4/4         all  (PASS)
Warning events      1           <10  (PASS)

TOP RESTART OFFENDERS
  1. worker-7d9f8b  42 restarts
  2. api-56c9d      8 restarts

NOTABLE EVENTS (last <window>)
  BackOff   pod/worker-7d9f8b  Back-off restarting failed container
```

Use unicode box-drawing or plain ASCII depending on terminal support. Keep the table aligned.

If the namespace has 0 pods, output PASS with a note: "No pods found in namespace <ns>."
