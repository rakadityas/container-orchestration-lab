# 1. Why Kubernetes Exists

## The big idea, in simple words

In the [Docker course](../docker-crash-course/01-containers-vs-vms.md) you
learned that a container is like **renting one room** in an apartment
building instead of building a whole new house.

That worked well for one machine. But now imagine you own **many buildings**
with thousands of rooms, and you need someone to:

- notice when a tenant moves out and immediately find a new one
- notice when a tenant is sick and quietly replace them
- tell visitors which room to go to, even when tenants move around
- add more rooms when the building gets crowded
- renovate rooms one at a time, so the building never fully closes

**Kubernetes is that building manager.**

You do not phone the manager every morning with instructions. Instead you
give the manager one written rule:

> "I always want 3 apartments occupied by this kind of tenant."

Then the manager checks, all day and all night, whether reality matches your
rule. If a tenant leaves, the manager fills the room. You never asked. It
just happens.

## The most important concept: the control loop

Almost everything in Kubernetes works the same way. This one pattern
explains 90% of its behaviour:

```
        ┌─────────────────────────────────┐
        │                                 │
        ▼                                 │
  read DESIRED state                      │
  (what you wrote in YAML)                │
        │                                 │
        ▼                                 │
  read ACTUAL state                       │
  (what is really running)                │
        │                                 │
        ▼                                 │
  are they different?                     │
        │                                 │
        ├── yes → take a small action ────┘
        │          to fix the difference
        │
        └── no  → do nothing, check again
```

This loop never stops. It is called **reconciliation**.

A good comparison is a **thermostat**. You set it to 21°C. You do not tell it
"turn on the heater now". It compares the room temperature to your number,
again and again, and turns the heater on or off by itself. If someone opens
a window, the thermostat reacts without being asked.

Kubernetes is a thermostat for your applications.

## Declarative vs imperative

This is the mental shift that confuses most people coming from Docker.

**Imperative** means you give **commands**. You say *how* to do it, step by
step. Docker is mostly imperative:

```bash
docker run -d --name api -p 8080:8080 myimage:v1   # do this, now
```

If that container dies at 3 AM, nothing brings it back. You gave a command
once, and the command finished.

**Declarative** means you describe the **end result**. You say *what* you
want, not how to reach it:

```yaml
# I want 3 copies of this running. Always. Figure out the rest.
spec:
  replicas: 3
```

If a container dies at 3 AM, the control loop notices that actual (2) does
not match desired (3), and starts a new one. Nobody is woken up.

A simple way to feel the difference:

| | You say | If something breaks |
|---|---|---|
| **Imperative** | "Start 3 containers" | Nothing happens. You must notice and fix it. |
| **Declarative** | "3 containers should exist" | Kubernetes fixes it automatically. |

This is why we `kubectl apply` a file instead of running commands. The file
is the rule. You can commit it to git, review it, and re-apply it safely a
hundred times — the result is always the same.

> **Rule of thumb:** in real work, always write YAML files and use
> `kubectl apply -f`. Use direct commands like `kubectl run` only for quick
> experiments you plan to throw away.

## The parts of the building

Here is the whole course in one table. Do not try to memorise it now — each
row gets its own lesson.

| Kubernetes object | In the building story | Lesson |
|---|---|---|
| **Pod** | One apartment with one or more tenants inside | [2](02-pods-replicasets-deployments.md) |
| **ReplicaSet** | The rule: "keep exactly 3 apartments occupied" | [2](02-pods-replicasets-deployments.md) |
| **Deployment** | The manager who also handles renovations safely | [2](02-pods-replicasets-deployments.md) |
| **Service** | The reception desk that knows which room to send you to | [3](03-services-and-ingress.md) |
| **Ingress** | The front door of the building, from the street | [3](03-services-and-ingress.md) |
| **ConfigMap** | The public notice board | [4](04-config-and-secrets.md) |
| **Secret** | The (unfortunately weak) safe | [4](04-config-and-secrets.md) |
| **Namespace** | A separate floor of the building | [4](04-config-and-secrets.md) |
| **ServiceAccount + RBAC** | Who is allowed to hold which keys | [5](05-rbac-and-networkpolicy.md) |
| **NetworkPolicy** | Which rooms may phone which other rooms | [5](05-rbac-and-networkpolicy.md) |
| **Node** | One actual physical building | [9](09-autoscaling.md) |

## The cluster itself: who does the work

You do not need deep knowledge here, but two words appear constantly.

- **Control plane** — the manager's office. It holds the desired state and
  runs the control loops. The API server is the front desk of that office;
  every command you send goes there first.
- **Node** — a worker machine that actually runs your containers. Each node
  runs an agent called the **kubelet**, which is the caretaker on site.

```
        You
         │  kubectl apply -f deployment.yaml
         ▼
┌──────────────────────┐
│  Control plane       │   "the manager's office"
│  (API server, etcd,  │
│   controllers)       │
└──────────┬───────────┘
           │ "please run this pod"
           ▼
┌──────────────────────┐   ┌──────────────────────┐
│  Node 1  (kubelet)   │   │  Node 2  (kubelet)   │
│   ┌────┐  ┌────┐     │   │   ┌────┐             │
│   │Pod │  │Pod │     │   │   │Pod │             │
│   └────┘  └────┘     │   │   └────┘             │
└──────────────────────┘   └──────────────────────┘
```

One important detail: **you never talk to a node directly.** You only tell
the control plane what you want. It decides which node runs what. This is
why you can lose a whole node and your application survives — the manager
simply reopens those apartments in another building.

## kubectl basics

`kubectl` (say "cube control") is your telephone to the control plane. These
are the commands you will use every day.

### Looking at things

```bash
kubectl get pods                  # list pods in the current namespace
kubectl get pods -o wide          # also show node and IP
kubectl get pods -A               # every namespace
kubectl get deploy,svc,ingress    # several kinds at once

kubectl describe pod <name>       # full detail + recent events (VERY useful)
kubectl logs <pod>                # print the logs
kubectl logs -f <pod>             # follow the logs live
kubectl logs <pod> --previous     # logs of the CRASHED previous container
```

`kubectl describe` is the single most useful debugging command. The
**Events** section at the bottom usually tells you exactly what went wrong
(image could not be pulled, not enough memory, probe failed).

`kubectl logs --previous` is the second most useful. When a pod keeps
crashing and restarting, the current container has no logs yet — the reason
is in the *previous* one.

### Changing things

```bash
kubectl apply -f manifests/         # apply every file in a folder
kubectl delete -f manifests/        # remove them again
kubectl rollout status deploy/api   # watch a rollout finish
kubectl rollout undo deploy/api     # go back to the previous version
kubectl scale deploy/api --replicas=5
```

### Getting inside

```bash
kubectl exec -it <pod> -- sh              # open a shell (needs a shell in the image!)
kubectl port-forward svc/api 8080:8080    # reach a Service from your laptop
```

Remember from [Docker lesson 6](../docker-crash-course/06-image-security.md):
our image is **distroless**, so it has no shell. `kubectl exec ... sh` will
fail on it. That is expected and is a security feature. Use
`kubectl debug` to attach a temporary container with tools instead:

```bash
kubectl debug -it <pod> --image=busybox --target=api
```

### A safety habit: `--dry-run`

Before applying something, ask Kubernetes to check it without changing
anything:

```bash
kubectl apply -f deployment.yaml --dry-run=server
```

`--dry-run=server` sends it to the API server for real validation, but
changes nothing. It catches typos and invalid fields before they matter.

### Two shortcuts that save hours

```bash
alias k=kubectl
kubectl config set-context --current --namespace=demo   # stop typing -n demo
```

## What you will build in this course

By the end of [lesson 6](06-deploy-to-kind.md) you will run the **exact same
Go API** from the Docker course on a real Kubernetes cluster on your laptop,
with Postgres, Redis, an Ingress, health probes, autoscaling, and a safe
rolling update.

Everything you learned earlier still applies. The image does not change. The
Dockerfile does not change. Kubernetes only decides *where* and *how many*
copies run, and what happens when things go wrong.

Next: [Pods, ReplicaSets, and Deployments](02-pods-replicasets-deployments.md).
