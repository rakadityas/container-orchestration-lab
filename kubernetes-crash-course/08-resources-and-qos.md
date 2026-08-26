# 8. Resource Requests, Limits, and QoS

## The big idea, in simple words

Every apartment needs a share of the building's water and electricity. The
manager needs to know how much, for two different reasons.

- **`requests`** = the **reservation**. "This tenant needs at least this
  much." The manager uses it to decide **which building has room**.
- **`limits`** = the **hard ceiling**. "This tenant may never use more than
  this." Going over has consequences.

```yaml
resources:
  requests:            # what is RESERVED for me (used for scheduling)
    cpu: 100m
    memory: 128Mi
  limits:              # what I may NEVER exceed (enforced at runtime)
    memory: 256Mi
```

Think of a restaurant booking:

- `requests` = "a table for 4 is reserved for you". The restaurant will not
  accept bookings for a table that is already reserved.
- `limits` = "you may not seat more than 6 people at it".

## The units, because they are strange

### CPU is measured in "millicores"

```
1000m = 1 whole CPU core
 500m = half a core
 100m = one tenth of a core
```

`m` means "milli" (one thousandth). `cpu: 100m` and `cpu: 0.1` mean the same
thing. Most people write `100m` because it is harder to misread.

CPU is **shared over time**. `100m` does not mean you get a tiny slice of
silicon; it means you get roughly 10% of one core's time.

### Memory is measured in bytes, and the suffix matters

```
Mi = mebibyte = 1024 × 1024 bytes    ← use this one
M  = megabyte = 1000 × 1000 bytes
Gi = gibibyte = 1024 Mi
```

`128Mi` and `128M` are **not** equal. Use `Mi` and `Gi` consistently.

## CPU and memory behave completely differently

This is the most important part of the lesson. Most people assume both work
the same way. They do not.

| | CPU | Memory |
|---|---|---|
| Can it be taken back? | **Yes** — "compressible" | **No** — "incompressible" |
| What happens at the limit | The app is **slowed down** (throttled) | The app is **killed** (`OOMKilled`) |
| Recovers on its own? | Yes, immediately | No, the process is dead |
| Severity | Annoying | Fatal |

Why the difference? CPU time can simply be given to someone else next
millisecond. But memory that is already written cannot be un-given. The
kernel's only option is to kill something.

**Result:** exceeding a CPU limit makes your app slow. Exceeding a memory
limit makes your app **die**.

```bash
kubectl get pod <name> -n demo -o jsonpath='{.status.containerStatuses[0].lastState}'
# look for: "reason":"OOMKilled"
```

An `OOMKilled` pod that restarts repeatedly shows as `CrashLoopBackOff`, and
the memory reason is easy to miss. Always check `lastState`.

## Why you should usually NOT set a CPU limit

This surprises people, so here is the reasoning.

When a container reaches its CPU limit, the Linux kernel **throttles** it: it
pauses the container for the rest of the current 100ms window. Even if the
whole machine is idle.

So `cpu: limit 500m` means: "even if 15 cores are free, this container waits."

For a web API this causes strange, hard-to-debug latency spikes. A request
that normally takes 5ms suddenly takes 100ms, because the container was
paused mid-request.

The common recommendation today:

| Resource | requests | limits |
|---|---|---|
| **CPU** | ✅ Always set | ❌ Usually leave empty |
| **Memory** | ✅ Always set | ✅ Always set, equal to requests |

- **CPU request but no limit:** the pod is guaranteed its share, and may use
  spare capacity when the machine is idle. Other pods are still protected,
  because requests reserve their share.
- **Memory request = limit:** predictable. The pod cannot cause other pods to
  be evicted, and you find out early if your memory estimate is wrong.

This is not a universal law — some teams set CPU limits for strict
multi-tenant fairness or predictable benchmarking. But "requests always,
CPU limits rarely" is a good default.

## QoS classes: who gets evicted first

When a node runs out of memory, Kubernetes must remove some pods. It decides
the order using the **QoS class**, which you never set directly — it is
derived from your requests and limits.

### 1. `Guaranteed` — safest

Every container has requests **and** limits, and they are **equal**, for both
CPU and memory.

```yaml
resources:
  requests: { cpu: 100m, memory: 256Mi }
  limits:   { cpu: 100m, memory: 256Mi }
```

Evicted **last**. Use for databases and critical workloads.

### 2. `Burstable` — the normal case

Requests are set, but limits are missing or larger than requests.

```yaml
resources:
  requests: { cpu: 100m, memory: 128Mi }
  limits:   { memory: 256Mi }
```

Evicted **second**. This is where most applications should be, and where our
API sits.

### 3. `BestEffort` — worst

**No requests and no limits at all.**

```yaml
# resources: {}   ← nothing specified
```

Evicted **first**, immediately, whenever a node is under pressure.

```bash
kubectl get pod <name> -n demo -o jsonpath='{.status.qosClass}'
```

```
Node is running out of memory. Eviction order:

  BestEffort   ← killed first  (you asked for nothing, you get nothing)
  Burstable    ← killed next, biggest overuse first
  Guaranteed   ← killed last
```

> **Never ship a pod with no resources set.** It is `BestEffort`, and it will
> be the first thing killed at the worst possible moment. Setting even rough
> numbers is far better than setting none.

## What happens when nothing fits

If no node has enough **unreserved** capacity for your requests, the pod
stays `Pending` forever:

```bash
kubectl describe pod <name> -n demo
```

```
Events:
  Warning  FailedScheduling  0/3 nodes are available:
           3 Insufficient cpu.
```

Important detail: scheduling uses **requests**, not actual usage. A node
whose pods requested everything but are using almost nothing is still
"full". Kubernetes honours reservations, exactly like a restaurant that
will not seat you at a reserved empty table.

## Choosing numbers

Do not guess forever. Use this loop:

1. **Start with a rough estimate.** For a small Go API: `cpu: 100m`,
   `memory: 128Mi`.
2. **Measure what it really uses** under normal load:
   ```bash
   kubectl top pods -n demo        # needs metrics-server installed
   ```
3. **Set requests near the normal usage**, with a little headroom.
4. **Set the memory limit** roughly 1.5–2× the request, to survive spikes.
5. **Watch for `OOMKilled`.** If you see it, your estimate was too low.

Two failure modes to balance:

| Too low | Too high |
|---|---|
| Pods get `OOMKilled` | Nodes look "full" while doing nothing |
| CPU throttling, slow responses | You pay for capacity nobody uses |
| Evicted early under pressure | Autoscaling triggers too late |

## Namespace-wide guard rails

Two objects stop one team from consuming a whole cluster.

**`ResourceQuota`** — a total budget for the namespace:

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: demo-quota
  namespace: demo
spec:
  hard:
    requests.cpu: "4"
    requests.memory: 8Gi
    limits.memory: 16Gi
    pods: "20"
```

Note the side effect: **once a quota exists, every pod must declare
resources.** A `BestEffort` pod is rejected. This is a useful way to enforce
good behaviour.

**`LimitRange`** — defaults for pods that forgot:

```yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: demo-defaults
  namespace: demo
spec:
  limits:
    - type: Container
      default:                 # applied as `limits` if missing
        cpu: 500m
        memory: 512Mi
      defaultRequest:          # applied as `requests` if missing
        cpu: 100m
        memory: 128Mi
```

## Our API's settings

```yaml
resources:
  requests:
    cpu: 100m        # 10% of a core reserved
    memory: 128Mi
  limits:
    memory: 256Mi    # memory ceiling; NO cpu limit on purpose
```

This gives QoS class `Burstable`: guaranteed a share, allowed to use spare
CPU when the node is idle, and unable to eat the node's memory.

There is one more reason requests matter, and it is the subject of the next
lesson: **the HorizontalPodAutoscaler measures CPU usage as a percentage of
the request.** Without a CPU request, autoscaling on CPU cannot work at all.

Next: [Autoscaling](09-autoscaling.md).
