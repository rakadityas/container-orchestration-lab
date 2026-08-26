# 10. Rollouts, Disruption, and Spreading Out

## The big idea, in simple words

Three related questions, all about **not losing all your pods at once**:

1. When I deploy a new version, how do I replace pods **without dropping
   requests**? → rollout strategy
2. When an administrator drains a node for maintenance, how do I stop them
   from removing **too many at the same time**? → PodDisruptionBudget
3. How do I make sure my pods are not **all in the same building**, so one
   power cut does not kill everything? → spreading

## Part 1: rollout strategies

### `RollingUpdate` (the default)

Replace pods gradually. The service never goes down.

```yaml
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1          # may create 1 EXTRA pod above `replicas`
      maxUnavailable: 0    # never have fewer than `replicas` ready
```

These two numbers control the whole process.

| Setting | Meaning | Effect |
|---|---|---|
| `maxSurge` | Extra pods allowed **above** the desired count | Higher = faster rollout, more capacity used |
| `maxUnavailable` | Pods allowed to be **missing** below the desired count | 0 = never lose capacity, but needs room to surge |

Both accept a number (`1`) or a percentage (`25%`).

**`maxSurge: 1, maxUnavailable: 0`** is the safest setting, and what we use.
Read it as: *"add the new one first, and only then remove an old one."* You
always have full capacity.

```
replicas: 3, maxSurge: 1, maxUnavailable: 0

start:  [v1] [v1] [v1]              3 ready
step 1: [v1] [v1] [v1] [v2]         add new first (4 pods briefly)
step 2: [v1] [v1] [v2]              now remove an old one
step 3: [v1] [v1] [v2] [v2]
step 4: [v1] [v2] [v2]
step 5: [v1] [v2] [v2] [v2]
done:   [v2] [v2] [v2]              never below 3 ready
```

The cost is that you briefly need capacity for one extra pod. The default
(`25%` / `25%`) is faster but can serve traffic with less capacity during
the rollout.

> A rollout only progresses when new pods become **Ready**
> ([lesson 7](07-probes.md)). Without a readiness probe, Kubernetes assumes
> a pod is ready the moment it starts, and will happily replace every
> working pod with broken ones. **Readiness probes are what make rollouts
> safe.**

If new pods never become ready, the rollout stops instead of destroying
everything:

```yaml
spec:
  progressDeadlineSeconds: 600   # give up after 10 minutes
```

### `Recreate`

```yaml
spec:
  strategy:
    type: Recreate
```

Kill **all** old pods, then start new ones. There is **downtime**.

Use it only when two versions genuinely cannot run at the same time — for
example an incompatible database schema change, or a single writer holding
an exclusive lock.

### Watching and undoing

```bash
kubectl rollout status deploy/api -n demo      # blocks until done
kubectl rollout history deploy/api -n demo
kubectl rollout undo deploy/api -n demo        # back one version
kubectl rollout undo deploy/api -n demo --to-revision=3
kubectl rollout pause deploy/api -n demo       # freeze mid-rollout
kubectl rollout resume deploy/api -n demo
```

`rollout undo` is instant because the old ReplicaSet still exists at 0
replicas ([lesson 2](02-pods-replicasets-deployments.md)). Kubernetes just
scales it back up.

To record what changed, use annotations:

```yaml
metadata:
  annotations:
    kubernetes.io/change-cause: "upgrade to v2.1.0 - add rate limiting"
```

### Beyond built-in rollouts

Kubernetes only offers rolling updates. For **canary** (send 5% of traffic to
the new version) or **blue-green** (switch all traffic at once), use Argo
Rollouts or Flagger. They are worth knowing about, but not part of this
course.

## Part 2: PodDisruptionBudget

### Two kinds of disruption

| Kind | Examples | Can Kubernetes prevent it? |
|---|---|---|
| **Involuntary** | Node hardware fails, kernel panic, out of memory | ❌ No |
| **Voluntary** | `kubectl drain`, node upgrade, autoscaler removing a node | ✅ **Yes — this is what a PDB protects** |

A **PodDisruptionBudget** only limits **voluntary** disruption. It cannot
stop a machine from catching fire.

### The problem it solves

You have 3 API pods spread over 3 nodes. An administrator upgrades the
cluster and drains all three nodes at once. All 3 pods are evicted
simultaneously. Your service is down, even though nothing failed.

A PDB is the building rule: **"never empty more than one apartment at a time
during maintenance."**

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: api
  namespace: demo
spec:
  minAvailable: 2          # at least 2 must stay running
  selector:
    matchLabels:
      app: api
```

Now `kubectl drain` **blocks** rather than taking you below 2. The
administrator must wait for replacements to become ready elsewhere.

You can express it either way:

```yaml
  minAvailable: 2          # keep at least 2
  # or
  maxUnavailable: 1        # remove at most 1 at a time
```

Percentages work too (`minAvailable: 50%`), which is better when the replica
count changes with an HPA.

### The trap: a PDB that blocks maintenance forever

```yaml
spec:
  replicas: 1          # only one pod
---
spec:
  minAvailable: 1      # and it must never go away
```

This makes draining the node **impossible**. Cluster upgrades hang, and the
autoscaler cannot remove nodes. Somebody will eventually delete your PDB in
frustration.

Rules to avoid this:

- With `replicas: 1`, either use no PDB, or `maxUnavailable: 1` (which
  permits eviction), and accept the brief downtime.
- Keep `minAvailable` **strictly below** `replicas`. With 3 replicas, use
  `minAvailable: 2`, never 3.

```bash
kubectl get pdb -n demo
```

```
NAME   MIN AVAILABLE   ALLOWED DISRUPTIONS   AGE
api    2               1                     5m
```

`ALLOWED DISRUPTIONS: 0` means maintenance is currently blocked. If it stays
at 0, investigate — that is usually a bug in your setup, not a healthy
state.

## Part 3: spreading pods out

Three replicas give you nothing if all three are on the **same node**. One
node failure and you are down.

### `topologySpreadConstraints` (the modern way)

```yaml
spec:
  template:
    spec:
      topologySpreadConstraints:
        # spread evenly across availability ZONES
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app: api
        # and also across NODES
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app: api
```

Reading the fields:

| Field | Meaning |
|---|---|
| `topologyKey` | What to spread across — a node label. Zone or hostname. |
| `maxSkew` | Biggest allowed difference between the fullest and emptiest group |
| `whenUnsatisfiable` | What to do if the rule cannot be met |

`maxSkew: 1` with 3 zones and 3 pods gives 1 pod per zone. With 4 pods it
gives 2/1/1 — a difference of 1, which is allowed.

The `whenUnsatisfiable` choice matters a lot:

| Value | Behaviour |
|---|---|
| `ScheduleAnyway` | Prefer to spread, but **run the pod anyway** if impossible (soft) |
| `DoNotSchedule` | Leave the pod `Pending` rather than break the rule (hard) |

> **Use `ScheduleAnyway` unless you are certain.** `DoNotSchedule` can leave
> pods `Pending` during an incident — exactly when you most need them
> running. Spreading is a preference; availability is the goal.

### `podAntiAffinity` (the older way)

```yaml
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app: api
                topologyKey: kubernetes.io/hostname
```

This says: "try hard not to put two `app: api` pods on the same node."

The two long field names mean:

- `preferredDuring...` = **soft**. Try, but schedule anyway if not possible.
- `requiredDuring...` = **hard**. Never break this rule, even if the pod
  stays `Pending`.

> `requiredDuringSchedulingIgnoredDuringExecution` with
> `topologyKey: hostname` means you can never have more pods than nodes. An
> HPA scaling to 10 pods on a 3-node cluster leaves 7 pods `Pending`
> forever. This is a very common self-inflicted outage.

### Which should I use?

| | topologySpreadConstraints | podAntiAffinity |
|---|---|---|
| Even distribution | ✅ Yes, that is its purpose | ❌ Only "avoid", not "balance" |
| Control how uneven | ✅ `maxSkew` | ❌ All or nothing |
| Performance at scale | ✅ Better | ⚠️ Slow with many pods |

**Prefer `topologySpreadConstraints`.** `podAntiAffinity` still appears in
older manifests and remains useful for "never co-locate these two different
apps".

### One more safety net

Spreading only helps if the *nodes* are spread. On a cloud cluster, ensure
your node group covers several availability zones. Three nodes in one zone
is still one power failure away from a full outage.

## Putting it together

For a service you cannot afford to drop:

```yaml
# 1. safe rollouts
strategy:
  rollingUpdate: { maxSurge: 1, maxUnavailable: 0 }

# 2. readiness probe, so rollouts wait for real readiness
readinessProbe: { httpGet: { path: /readyz, port: 8080 } }

# 3. survive maintenance
kind: PodDisruptionBudget
spec: { minAvailable: 2 }

# 4. survive losing a node or a zone
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: ScheduleAnyway
```

There is still one gap. Even with all of the above, a pod being removed can
drop the requests it was serving at that moment. Fixing that is the next
lesson.

Next: [Graceful shutdown](11-graceful-shutdown.md).
