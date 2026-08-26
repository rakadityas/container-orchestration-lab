# 9. Autoscaling: Pods and Nodes

## The big idea, in simple words

The building is getting crowded. There are two completely different answers,
and you need both:

| Problem | Answer | Tool |
|---|---|---|
| Too many visitors for the apartments we have | **Open more apartments** | HorizontalPodAutoscaler |
| We have no empty apartments left to open | **Build another building** | Cluster Autoscaler / Karpenter |

These are **separate problems** solved by **separate tools**. This confuses
many people, so keep them apart:

```
traffic increases
       │
       ▼
HPA adds pods ─────────────► are there nodes with free space?
                                    │              │
                                   yes            no
                                    │              │
                                    ▼              ▼
                              pods start     pods stuck "Pending"
                                                   │
                                                   ▼
                                    Cluster Autoscaler / Karpenter
                                          adds a NODE
                                                   │
                                                   ▼
                                            pods start
```

**HPA alone is not enough** in the cloud. Without node scaling, HPA creates
pods that sit in `Pending` forever because nothing has room for them.

## Part 1: HorizontalPodAutoscaler (more pods)

### Horizontal vs vertical

| | Meaning | Building story |
|---|---|---|
| **Horizontal** | More copies of the pod | Open more apartments |
| **Vertical** | Make each pod bigger | Make one apartment bigger |

Horizontal is almost always the right answer for web services. It handles
node failure better and has no upper limit, while a single pod can never be
bigger than one machine.

### It needs metrics-server

The HPA reads CPU and memory usage from an add-on called **metrics-server**.
It is **not installed by default** in kind.

```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# in kind ONLY: the kubelet uses self-signed certs, so allow insecure TLS
kubectl patch deployment metrics-server -n kube-system --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'

kubectl wait --for=condition=Available deploy/metrics-server -n kube-system --timeout=120s
kubectl top nodes      # should print numbers now
kubectl top pods -n demo
```

Do **not** use `--kubelet-insecure-tls` in a real cluster. It is a local
workaround only.

### A basic HPA

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api
  namespace: demo
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api            # which Deployment to scale
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70     # aim for 70% CPU
```

Use `autoscaling/v2`, not the old `v1`. Only v2 supports memory, multiple
metrics, and scaling behaviour.

### ⚠️ `averageUtilization` is a percentage of the REQUEST

This is the detail that breaks most first attempts.

`averageUtilization: 70` does **not** mean 70% of a CPU core. It means **70%
of the pod's CPU request** from [lesson 8](08-resources-and-qos.md).

```
request = 100m
target  = 70%
        → HPA aims for 70m of actual CPU use per pod
```

Two direct consequences:

1. **No CPU request = no CPU autoscaling.** The HPA has nothing to calculate
   a percentage of. It reports `<unknown>` and never scales.
2. **Changing the request changes the scaling behaviour**, even if you never
   touch the HPA.

### The formula

```
desiredReplicas = ceil( currentReplicas × ( currentUsage / targetUsage ) )
```

Example: 3 pods, each using 140m, target 70m.

```
ceil( 3 × (140 / 70) ) = ceil(3 × 2) = 6 pods
```

Kubernetes ignores changes smaller than 10%, so it does not add and remove
pods constantly over tiny fluctuations.

### Scaling up fast, scaling down slowly

By default, the HPA is eager to add pods and careful about removing them.
That asymmetry is deliberate: adding a pod you did not need costs a little
money, but removing a pod you did need causes an outage.

You can control it:

```yaml
spec:
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 0     # react immediately
      policies:
        - type: Percent
          value: 100                    # allowed to DOUBLE
          periodSeconds: 15
    scaleDown:
      stabilizationWindowSeconds: 300   # wait 5 min of calm before shrinking
      policies:
        - type: Percent
          value: 50                     # remove at most half
          periodSeconds: 60
```

`stabilizationWindowSeconds` on `scaleDown` is the important one. It stops
**flapping** — adding and removing pods repeatedly because traffic goes up
and down every minute. The HPA looks at the highest recommendation from the
last 5 minutes before shrinking.

### Scaling on memory (usually a mistake)

```yaml
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```

Be careful. Many applications (especially with garbage collection, like Go
and Java) hold memory and never release it. Memory usage goes up and stays
up, so the HPA adds pods forever and never removes them.

**CPU is usually the better signal** for a web API.

### Scaling on real-world metrics

CPU is a rough substitute for what you actually care about. Better signals
are requests per second, or queue length.

With `type: External` (and a metrics adapter like Prometheus Adapter, or
**KEDA**), you can scale on things such as:

- HTTP requests per second
- messages waiting in an SQS or Kafka queue
- active database connections

For a queue worker, queue length is far better than CPU — and KEDA can even
scale to **zero** pods when the queue is empty, which the standard HPA
cannot.

### HPA and VPA do not mix

The **VerticalPodAutoscaler** changes requests and limits instead of the
number of pods.

> **Do not use VPA and HPA on CPU for the same workload.** The HPA scales on
> a percentage of the request, and the VPA changes the request. They fight
> each other and cause strange oscillation.

VPA is useful for right-sizing things you do not scale horizontally, such as
a single database pod.

### Watching it work

```bash
kubectl get hpa -n demo -w
```

```
NAME   REFERENCE        TARGETS         MINPODS   MAXPODS   REPLICAS
api    Deployment/api   45%/70%         2         10        3
```

`TARGETS` shows `current/target`. If it says `<unknown>/70%`, then either
metrics-server is missing or your pods have no CPU request.

Generate some load and watch:

```bash
# terminal 1
kubectl get hpa,pods -n demo -w

# terminal 2 — hammer the API from inside the cluster
kubectl run load --rm -it --restart=Never -n demo --image=busybox -- \
  sh -c 'while true; do wget -q -O- http://api/items > /dev/null; done'
```

Watch `TARGETS` climb past 70%, then `REPLICAS` increase. Stop the load
generator with Ctrl+C, and watch it shrink again after the 5-minute
stabilisation window. The wait is intentional — do not assume it is broken.

## Part 2: Node autoscaling (more buildings)

HPA adds pods. But pods need somewhere to run. When every node is full, new
pods stay `Pending`:

```
Events:
  Warning  FailedScheduling  0/3 nodes are available: 3 Insufficient cpu.
```

This is the signal for a **node autoscaler**. There are two main options.

### Cluster Autoscaler — the older, standard one

It watches for `Pending` pods and adds nodes to a pre-defined group (an AWS
Auto Scaling Group, for example).

```
Pending pod detected
        ↓
find a node group whose instance type would fit it
        ↓
increase that group's desired count by 1
        ↓
new node joins, pod is scheduled
```

Its limitation: it can only change the **number** of nodes in groups you
defined in advance. You must decide the instance types yourself. If you
configured `m5.large` groups and a pod needs a GPU, it cannot help.

It also removes nodes that stay underused (by default, below 50% for 10
minutes) when their pods can fit elsewhere.

### Karpenter — the newer approach

Karpenter (originally from AWS) does not use fixed node groups. It looks at
the `Pending` pods and **provisions the right instance type on demand**.

```
Pending pods need 6 CPU and 12Gi total
        ↓
Karpenter checks live EC2 prices and availability
        ↓
launches ONE well-fitting instance, possibly a Spot instance
        ↓
pods are scheduled in ~40 seconds
```

| | Cluster Autoscaler | Karpenter |
|---|---|---|
| Instance types | Fixed groups you define | Chosen automatically per need |
| Speed | Minutes | Often under a minute |
| Bin packing | Limited by group shapes | Actively packs pods well |
| Spot instances | Possible, more manual | Built in, with interruption handling |
| Consolidation | Removes underused nodes | Actively **replaces** nodes with cheaper ones |
| Works on | Many clouds | Mainly AWS (others growing) |

Karpenter's **consolidation** is the biggest practical difference: it
notices that your workload would fit on one cheaper instance and moves it,
which typically cuts cost significantly.

A small Karpenter example, to show the shape:

```yaml
apiVersion: karpenter.sh/v1beta1
kind: NodePool
metadata:
  name: default
spec:
  template:
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["spot", "on-demand"]     # prefer cheap Spot capacity
        - key: kubernetes.io/arch
          operator: In
          values: ["arm64", "amd64"]
  limits:
    cpu: 100                                 # never exceed 100 cores total
  disruption:
    consolidationPolicy: WhenUnderutilized   # actively shrink and replace
```

> Note `arch: arm64`. This connects to
> [Docker lesson 3](../docker-crash-course/03-multistage-builds-go.md): if
> Karpenter may pick cheaper ARM instances, your image **must** be built for
> `arm64` too. This is why the Dockerfile uses `TARGETARCH` instead of a
> hardcoded `amd64`, and why multi-architecture images matter in production.

### None of this works on kind

kind runs nodes as containers on your laptop. There is no cloud API to ask
for more machines. Node autoscaling is a cloud topic — understand the
concept now, use it when you run on EKS or GKE.

## The three autoscalers, side by side

| | Changes | Reacts to | Use for |
|---|---|---|---|
| **HPA** | Number of pods | CPU, memory, custom metrics | Web services under changing load |
| **VPA** | Size of pods | Historical usage | Right-sizing single-instance workloads |
| **Cluster Autoscaler / Karpenter** | Number of nodes | `Pending` pods | Making room for whatever HPA created |

A healthy production setup uses **HPA + a node autoscaler**, with VPA left
off or used only in recommendation mode.

Next: [Rollouts and disruption](10-rollouts-and-disruption.md).
