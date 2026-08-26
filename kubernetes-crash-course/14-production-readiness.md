# 14. Lab: Production Readiness (+ 10-Question Quiz)

This is the hands-on lesson for Part 2. You will upgrade the Day-1 deployment
into a production-ready one, trigger a rolling update, and watch every
mechanism from Part 2 do its job.

## Step 1: install metrics-server

The HPA cannot work without it ([lesson 9](09-autoscaling.md)).

```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# kind only: the kubelet uses self-signed certificates
kubectl patch deployment metrics-server -n kube-system --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'

kubectl wait --for=condition=Available deploy/metrics-server -n kube-system --timeout=120s
kubectl top pods -n demo      # must print numbers before continuing
```

## Step 2: apply the production version

```bash
kubectl apply -f manifests/production/
kubectl rollout status deploy/api -n demo
```

Open [`manifests/production/api-deployment.yaml`](manifests/production/api-deployment.yaml)
next to [`manifests/base/05-api.yaml`](manifests/base/05-api.yaml) and compare
them. Everything that was added:

| Added | Lesson | Why |
|---|---|---|
| `startupProbe`, `readinessProbe`, `livenessProbe` | [7](07-probes.md) | Know the difference between broken and busy |
| `resources.requests` / `limits` | [8](08-resources-and-qos.md) | Scheduling, QoS class, and HPA maths |
| `HorizontalPodAutoscaler` | [9](09-autoscaling.md) | Add pods under load |
| `strategy: maxSurge 1 / maxUnavailable 0` | [10](10-rollouts-and-disruption.md) | Never lose capacity during a deploy |
| `PodDisruptionBudget` | [10](10-rollouts-and-disruption.md) | Survive node maintenance |
| `topologySpreadConstraints` | [10](10-rollouts-and-disruption.md) | Survive losing a node or zone |
| `preStop` + `terminationGracePeriodSeconds` | [11](11-graceful-shutdown.md) | Zero dropped requests |
| `securityContext` | [5](05-rbac-and-networkpolicy.md) | Enforce non-root at the cluster level |

Check the results:

```bash
kubectl get hpa -n demo          # TARGETS should show a real percentage
kubectl get pdb -n demo          # ALLOWED DISRUPTIONS should be >= 1
kubectl get pods -n demo -o wide # pods spread across different nodes
kubectl get pod <name> -n demo -o jsonpath='{.status.qosClass}'   # Burstable
```

## Step 3: watch a rolling update with zero dropped requests

This is the most valuable experiment in the whole course.

```bash
# terminal 1 — constant traffic, printing ONLY failures
kubectl run curl-loop --rm -it --restart=Never -n demo --image=busybox -- \
  sh -c 'while true; do wget -q -O- http://api/healthz >/dev/null || echo "FAILED $(date +%T)"; sleep 0.1; done'
```

```bash
# terminal 2 — watch the pods change
kubectl get pods -n demo -w
```

```bash
# terminal 3 — force a full rollout
kubectl rollout restart deploy/api -n demo
kubectl rollout status deploy/api -n demo
```

Watch terminal 2 carefully. You will see the pattern from
[lesson 10](10-rollouts-and-disruption.md): a **new** pod appears and becomes
`Running` **before** an old one starts `Terminating`. That is
`maxSurge: 1, maxUnavailable: 0`.

Terminal 1 should print **nothing at all**.

### Now break it on purpose

Remove the graceful shutdown and see the difference:

```bash
kubectl patch deploy/api -n demo --type=json \
  -p='[{"op":"remove","path":"/spec/template/spec/containers/0/lifecycle"}]'

kubectl rollout restart deploy/api -n demo
```

Now terminal 1 will usually print a burst of `FAILED` lines each time a pod
terminates. Those are the dropped requests caused by the endpoint-removal
race.

Put it back:

```bash
kubectl apply -f manifests/production/
```

This single experiment is the difference between a deployment your users
notice and one they do not.

## Step 4: watch autoscaling

```bash
# terminal 1
kubectl get hpa,pods -n demo -w

# terminal 2 — generate load
kubectl run load --rm -it --restart=Never -n demo --image=busybox -- \
  sh -c 'while true; do wget -q -O- http://api/items >/dev/null; done'
```

Watch `TARGETS` climb above 70%, then `REPLICAS` increase, then new pods
appear. Stop the load with Ctrl+C.

**Scaling down takes 5 minutes.** That is the
`scaleDown.stabilizationWindowSeconds: 300` setting. It is intentional. Do
not assume something is broken.

## Step 5: test disruption protection

```bash
kubectl get pdb -n demo
kubectl drain <node-name> --ignore-daemonsets --delete-emptydir-data --dry-run=client
```

Use `kubectl get nodes` to pick a worker node name. The PDB ensures a real
drain cannot take you below 2 available pods.

## The production readiness checklist

Before any real deployment, check every line:

**Health and traffic**
- [ ] Readiness probe exists and checks real dependencies
- [ ] Liveness probe exists **and does not check external dependencies**
- [ ] Startup probe if the app takes more than a few seconds to boot

**Resources**
- [ ] CPU and memory **requests** set on every container
- [ ] Memory **limit** set
- [ ] QoS class is `Burstable` or `Guaranteed`, never `BestEffort`

**Scaling**
- [ ] `minReplicas` is at least 2
- [ ] HPA target chosen deliberately, and CPU requests exist for it to use
- [ ] Node autoscaling exists in the cloud (Cluster Autoscaler or Karpenter)

**Deploying safely**
- [ ] `maxUnavailable: 0` for services that must not lose capacity
- [ ] `preStop` wait and a matching `terminationGracePeriodSeconds`
- [ ] The application handles SIGTERM and drains in-flight requests
- [ ] `ENTRYPOINT` uses exec form, so the signal reaches the process

**Surviving failure**
- [ ] PodDisruptionBudget, with `minAvailable` **below** the replica count
- [ ] `topologySpreadConstraints` across zones, using `ScheduleAnyway`
- [ ] Nodes actually exist in more than one zone

**Security**
- [ ] `runAsNonRoot`, `readOnlyRootFilesystem`, all capabilities dropped
- [ ] Its own ServiceAccount, with `automountServiceAccountToken: false`
- [ ] No RBAC permissions unless genuinely required
- [ ] Default-deny NetworkPolicy, plus explicit allow rules **and DNS**
- [ ] Real secrets come from a secrets manager, not a committed YAML file
- [ ] Image tagged by digest or commit SHA, never `:latest`

---

# Quiz: 10 Questions

Answer from memory first. The answers follow, with the reasoning.

**1.** Your app's `/healthz` endpoint pings Postgres, and you use it as the
liveness probe. Postgres becomes slow for 60 seconds. What happens, and why
is it bad?

**2.** You set `resources.limits.cpu: 500m`. The node is almost idle, but
your API has latency spikes. Why?

**3.** What is the difference between a readiness probe failing and a
liveness probe failing?

**4.** Your HPA shows `TARGETS: <unknown>/70%` and never scales. Give two
possible causes.

**5.** During every deployment, users see a few HTTP errors, even though
`maxUnavailable: 0` and the app handles SIGTERM correctly. What is missing?

**6.** You have `replicas: 1` and a PDB with `minAvailable: 1`. What breaks?

**7.** Why do we use a StatefulSet for Postgres but a Deployment for Redis in
this project, when both store data?

**8.** A colleague says "Secrets are safe because they are encoded". What is
wrong, and what should be used instead?

**9.** You apply a default-deny NetworkPolicy. The app immediately fails
with errors resolving `postgres`. What did you forget?

**10.** Your pods are `Pending` with "Insufficient cpu", but `kubectl top
nodes` shows CPU almost idle. Explain.

---

## Answers

**1.** Every API pod's liveness probe fails at the same time, so Kubernetes
**restarts all of them simultaneously**. They restart cold, all reconnect at
once, and that reconnect storm makes Postgres even slower — causing another
round of restarts. A 60-second database hiccup becomes a total outage that
cannot recover on its own.

Restarting your API cannot fix a sick database. Liveness must check only the
process itself; dependency checks belong in **readiness**, which stops
traffic without killing anything. ([lesson 7](07-probes.md))

**2.** CPU limits cause **throttling**. When the container reaches its limit
within a 100ms window, the kernel pauses it for the rest of that window —
even if the entire machine is idle. A request that normally takes 5ms can
suddenly take 100ms.

This is why the usual advice is: always set CPU **requests**, rarely set CPU
**limits**. ([lesson 8](08-resources-and-qos.md))

**3.** Readiness failing removes the pod's IP from the Service endpoints.
Traffic stops arriving, the container **keeps running**, and it rejoins
automatically when it recovers.

Liveness failing **kills and restarts the container**.

Readiness is safe and reversible. Liveness destroys a running process, so it
should be slower, more forgiving, and used only for genuine deadlocks.
([lesson 7](07-probes.md))

**4.** Either:
1. **metrics-server is not installed** (or unhealthy), so the HPA has no data
   at all; or
2. **the pods have no CPU `request`**. `averageUtilization` is a percentage
   *of the request*, so with no request there is nothing to compute.

([lesson 9](09-autoscaling.md))

**5.** A **`preStop` wait**. Removing the pod from Service endpoints happens
**in parallel** with SIGTERM, not before it, and takes a few seconds to
propagate to every node's kube-proxy. A well-behaved app that exits quickly
therefore exits *while traffic is still being routed to it*.

Add `preStop: sleep: seconds: 10`, and make sure
`terminationGracePeriodSeconds` is larger than the preStop wait plus the
app's shutdown time. ([lesson 11](11-graceful-shutdown.md))

**6.** **Node drains become impossible.** Evicting the single pod would take
you below `minAvailable: 1`, so the API server refuses. Cluster upgrades
hang, and the node autoscaler cannot remove nodes.

Keep `minAvailable` strictly below the replica count, or for a single replica
use `maxUnavailable: 1` and accept the brief downtime.
([lesson 10](10-rollouts-and-disruption.md))

**7.** Because of what the data is **worth**, not whether data exists.

Postgres holds durable records that must survive a restart, so it needs a
stable identity and its own disk — a StatefulSet with `volumeClaimTemplates`.

Redis here holds only hit counters, which are a disposable cache. Losing them
is acceptable, so a Deployment is fine. This is the same reasoning as "redis
has no volume" in the Docker Compose file.

If Redis were the source of truth for something, it would need a StatefulSet
too. ([lesson 12](12-statefulsets.md))

**8.** Base64 is **encoding, not encryption**. It is a costume, not a lock —
anyone with read access decodes it in one command with no key:
`kubectl get secret x -o jsonpath='{.data.y}' | base64 -d`.

The real protections are **RBAC** (who may read the Secret), **encryption at
rest** in etcd (which must be enabled by an administrator), and above all
keeping the real value in a proper secrets manager. Use **External Secrets
Operator** or the **AWS Secrets Manager CSI driver**, so what you commit to
git is only an *address*, never the secret. ([lesson 4](04-config-and-secrets.md))

**9.** **A rule allowing DNS.** A default-deny **egress** policy also blocks
traffic to the cluster DNS server in `kube-system`, so every hostname lookup
fails. Always pair default-deny with an explicit allow rule for UDP and TCP
port 53 to `kube-dns`.

This is the single most common NetworkPolicy mistake, and the errors look
nothing like a network policy problem. ([lesson 5](05-rbac-and-networkpolicy.md))

**10.** Scheduling uses **requests**, not actual usage. The node's capacity
is already **reserved** by existing pods' requests, even though those pods
are barely using it.

Kubernetes honours reservations like a restaurant that will not seat you at a
reserved but empty table. Either lower the requests (if they are unrealistic)
or add nodes. ([lesson 8](08-resources-and-qos.md))

---

## Clean up

```bash
kubectl delete namespace demo
kind delete cluster --name k8s-course
```

## Where to go next

You now have a production-shaped deployment. The natural next topics:

| Topic | What it adds |
|---|---|
| **Observability** | Prometheus, Grafana, OpenTelemetry — you cannot operate what you cannot see |
| **GitOps** | Argo CD or Flux — git as the single source of truth, no manual `kubectl apply` |
| **Progressive delivery** | Argo Rollouts, Flagger — canary and blue-green deployments |
| **Service mesh** | Istio, Linkerd — mTLS, retries, traffic splitting |
| **Policy** | Kyverno, OPA Gatekeeper — enforce the checklist above automatically |
| **Managed Kubernetes** | EKS, GKE — where node autoscaling and IAM integration become real |

Congratulations — you have finished the course.
