# 2. Pods, ReplicaSets, and Deployments

## The big idea, in simple words

These three objects sit on top of each other, like a small pyramid. Each one
adds one job.

| Object | Its one job | In the building story |
|---|---|---|
| **Pod** | Run the containers | One apartment with tenants inside |
| **ReplicaSet** | Keep the right *number* of pods | The rule: "always 3 apartments occupied" |
| **Deployment** | Change versions *safely* | The manager who renovates one room at a time |

```
Deployment  ("I manage versions and renovations")
    │  creates and controls
    ▼
ReplicaSet  ("I keep exactly 3 alive")
    │  creates and controls
    ▼
  Pod  Pod  Pod   ("we actually run the containers")
```

**You almost always write a Deployment.** The other two are created for you
automatically. But you must understand all three, because when something
breaks, you will read all three in `kubectl get`.

## The Pod

A **Pod** is the smallest thing Kubernetes can run. It is not the same as a
container. A Pod is a *wrapper* around one or more containers.

Containers in the same Pod:

- always run on the **same node** — they are never split up
- **share one IP address** and one network space
- can talk to each other over `localhost`
- can share storage volumes

Think of a Pod as **one apartment**. Usually one tenant lives there (one
container). Sometimes a helper lives there too — a cleaner, or an assistant.
That helper is called a **sidecar**. It shares the apartment, so it shares
the address and the front door.

**Rule for beginners: put one container in a Pod.** Add a second only when it
truly cannot work anywhere else (a log shipper, a proxy). If two containers
could run on different machines, they should be two separate Pods.

Here is a minimal Pod:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: api
spec:
  containers:
    - name: api
      image: docker-crash-course-api:dev
      ports:
        - containerPort: 8080
```

### Pods are disposable, and that is the point

This is the hardest idea for people coming from virtual machines.

**A Pod is never repaired. It is replaced.**

If a Pod becomes unhealthy, Kubernetes does not log in and fix it. It deletes
the Pod and creates a new one from the same recipe. The new Pod gets a
**new name** and a **new IP address**.

So you must never:

- write down a Pod's IP address anywhere
- store important files inside a Pod
- treat a Pod as a machine you can log into and repair

Think of Pods as **paper cups, not china cups**. When one is dirty, you throw
it away and take a new one. This is exactly the same lesson as the container
writable layer from
[Docker lesson 1](../docker-crash-course/01-containers-vs-vms.md).

### Never create a bare Pod yourself

If you create the Pod above with `kubectl apply` and then delete it, **it
stays deleted**. Nothing brings it back. Nobody is watching it.

A bare Pod has no manager. That is why we use the next two objects.

## The ReplicaSet

A **ReplicaSet** has exactly one job: make sure a certain number of identical
Pods exist.

Its control loop (see [lesson 1](01-why-kubernetes.md)) is very simple:

```
count the pods I own
compare to `replicas`
too few?  → create more
too many? → delete some
```

That is the entire job. Nothing else.

```yaml
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api        # "the pods I own are the ones with this label"
  template:           # the recipe for making a new pod
    metadata:
      labels:
        app: api      # this MUST match the selector above
    spec:
      containers:
        - name: api
          image: docker-crash-course-api:dev
```

### Labels and selectors: how objects find each other

This is a core Kubernetes idea and it appears everywhere.

Objects do **not** reference each other by name. They reference each other by
**label**, using a **selector**.

A label is just a sticky note: `app: api`. A selector is a search:
"everything wearing the sticky note `app: api`".

```
ReplicaSet selector:  app=api
                        │ finds
        ┌───────────────┼───────────────┐
        ▼               ▼               ▼
   ┌─────────┐    ┌─────────┐    ┌─────────┐
   │ Pod     │    │ Pod     │    │ Pod     │
   │ app=api │    │ app=api │    │ app=api │
   └─────────┘    └─────────┘    └─────────┘
```

This is loose and flexible: the ReplicaSet does not care which Pods exist, or
what they are called. It only counts labels.

> **Warning:** if `template.metadata.labels` does not match
> `selector.matchLabels`, the ReplicaSet creates Pods it does not recognise
> as its own. It then creates more, forever. This is a classic beginner bug.

### Why you still do not write ReplicaSets

A ReplicaSet can keep 3 Pods alive, but it cannot **change the version**
safely. If you edit the image, it will not update the running Pods in a
controlled way.

That missing skill is what a Deployment adds.

## The Deployment

A **Deployment** manages ReplicaSets, and ReplicaSets manage Pods.

Its extra skill is **safe version changes**. When you change the image, the
Deployment:

1. creates a **new** ReplicaSet for the new version
2. slowly scales the new one **up**
3. slowly scales the old one **down**
4. keeps the old one at 0 replicas, so you can go back instantly

```
Before:   ReplicaSet-v1 (3 pods)   ReplicaSet-v2 (0 pods)
During:   ReplicaSet-v1 (2 pods)   ReplicaSet-v2 (1 pod)
During:   ReplicaSet-v1 (1 pod)    ReplicaSet-v2 (2 pods)
After:    ReplicaSet-v1 (0 pods)   ReplicaSet-v2 (3 pods)
                        ↑ kept, so `rollout undo` is instant
```

The building manager renovates **one apartment at a time**, so tenants always
have somewhere to live. The building never closes.

Here is a real Deployment for our API:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  labels:
    app: api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: docker-crash-course-api:dev
          ports:
            - containerPort: 8080
          env:
            - name: PORT
              value: "8080"
```

Notice how similar this is to the ReplicaSet. The structure is identical —
`replicas`, `selector`, `template`. Only `kind:` changed. That is why people
say "just use a Deployment": you get everything a ReplicaSet does, plus
rollouts, for free.

## Watching a rollout happen

```bash
kubectl apply -f manifests/api-deployment.yaml
kubectl rollout status deploy/api        # blocks until finished

# change the image, then watch again
kubectl set image deploy/api api=docker-crash-course-api:v2
kubectl rollout status deploy/api

kubectl get rs                            # you now see TWO ReplicaSets
kubectl rollout history deploy/api        # the list of versions
kubectl rollout undo deploy/api           # go back one version
```

Run `kubectl get rs` after an update. Seeing the old ReplicaSet sitting at 0
replicas is the moment the whole model becomes clear.

The details of *how fast* pods are replaced (`maxSurge`, `maxUnavailable`)
are covered in [lesson 10](10-rollouts-and-disruption.md).

## Which controller should I use?

Deployments are not the only option. Pick by the shape of your workload:

| Controller | Use it for | Example |
|---|---|---|
| **Deployment** | Stateless apps where all copies are identical and interchangeable | Our Go API |
| **StatefulSet** | Apps needing a stable name and their own disk | Postgres — see [lesson 12](12-statefulsets.md) |
| **DaemonSet** | One copy on *every* node | Log collectors, monitoring agents |
| **Job** | Run once until it succeeds, then stop | A database migration |
| **CronJob** | Run on a schedule | A nightly cleanup |

Use a Deployment unless you have a specific reason not to. Roughly 90% of
workloads are Deployments.

## Reading a broken Pod

You will see these statuses constantly. Learn what they mean now, and you
will debug much faster later.

| Status | Meaning | Usual cause |
|---|---|---|
| `Pending` | Not placed on a node yet | No node has enough CPU/memory — see [lesson 8](08-resources-and-qos.md) |
| `ContainerCreating` | Node is pulling the image | Slow network, or a big image |
| `ImagePullBackOff` | Cannot download the image | Wrong name, wrong tag, or private registry with no credentials |
| `CrashLoopBackOff` | Starts, crashes, starts, crashes | The app itself is failing — read the logs |
| `Running` | Containers are up | Not the same as *working*! See probes, [lesson 7](07-probes.md) |
| `OOMKilled` | Used more memory than its limit | Raise the limit or fix a memory leak |

The debugging order is almost always the same:

```bash
kubectl describe pod <name>       # 1. read the Events at the bottom
kubectl logs <name>               # 2. read the app's own output
kubectl logs <name> --previous    # 3. if it crashed, read the DEAD one
```

**`CrashLoopBackOff` is not a Kubernetes error.** It means your program
started and then exited. The reason is in the logs, not in Kubernetes. The
word "BackOff" means Kubernetes is waiting longer and longer between retries
(10s, 20s, 40s...) so it does not waste the whole cluster restarting a
program that will fail again.

Next: [Services and Ingress](03-services-and-ingress.md).
