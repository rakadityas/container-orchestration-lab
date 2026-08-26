# Istio Crash Course

The third course in this series. In the
[Docker course](../docker-crash-course/), a container was **renting one
room**. In the [Kubernetes course](../kubernetes-crash-course/), Kubernetes
became **the building manager** who keeps apartments filled.

**Istio gives every apartment a personal assistant who stands at the door**
and handles every phone call on the tenant's behalf: proving who's calling,
encrypting the conversation, retrying if the line is busy, and writing down
what happened — all without the tenant (your application code) doing
anything differently.

This course installs Istio into the **same kind cluster** and **same
`demo` namespace** from the Kubernetes course, and runs the **exact same Go
API image**, with zero code or Dockerfile changes.

## Lessons

| # | Lesson | The idea in one line |
|---|---|---|
| 1 | [What Is a Service Mesh?](01-what-is-a-service-mesh.md) | An assistant at every door, so every service stops rewriting the same call-handling code |
| 2 | [Installing Istio](02-installing-istio.md) | Turning on sidecars, and the real collision with strict Pod security |
| 3 | [Traffic Management](03-traffic-management.md) | Gateway = front door, VirtualService = routing rules, DestinationRule = house rules |
| 4 | [Security and mTLS](04-security-mtls.md) | Checking ID at every door, then checking the rulebook too |
| 5 | [Resilience](05-resilience.md) | Retries, timeouts, circuit breaking — and how retries alone make an outage worse |
| 6 | [Observability](06-observability.md) | Every assistant was already watching every call — now you can see it too |
| 7 | [**Lab:** Full Mesh Setup + Canary](07-lab-full-mesh-setup.md) | Install, split traffic, lock down security, verify everything for real |

## What you need

```bash
brew install istioctl
```

Plus a running kind cluster from the Kubernetes course:

```bash
cd ../kubernetes-crash-course
kind create cluster --name k8s-course --config manifests/kind-config.yaml
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/calico.yaml
kubectl wait --for=condition=Ready pods --all -n kube-system --timeout=300s
kind load docker-image docker-crash-course-api:dev --name k8s-course
kubectl apply -f manifests/base/
kubectl get pods -n demo
```

## Quick start

```bash
cd ../istio-crash-course

# label the api deployment so DestinationRule subsets have something to match
kubectl patch deployment api -n demo --type=merge -p \
  '{"spec":{"template":{"metadata":{"labels":{"version":"v1"}}}}}'

# install Istio, sized for a laptop
istioctl install -f manifests/istio-install.yaml -y

# turn on sidecar injection and restart everything in demo
kubectl label namespace demo istio-injection=enabled
kubectl rollout restart deployment -n demo
kubectl rollout restart statefulset -n demo
kubectl get pods -n demo          # every pod should read 2/2

# open the front door and split traffic 90/10 between two versions
kubectl apply -f manifests/api-v2-deployment.yaml
kubectl apply -f manifests/gateway.yaml
kubectl apply -f manifests/destinationrule.yaml
kubectl apply -f manifests/virtualservice-canary.yaml
```

Full step-by-step walkthrough with verification at every stage:
[lesson 7](07-lab-full-mesh-setup.md).

## The manifests

```
manifests/
├── istio-install.yaml                    # laptop-sized IstioOperator profile
├── gateway.yaml                          # front door for api.local
├── api-v2-deployment.yaml                # second version, for canary demos
├── destinationrule.yaml                  # v1/v2 subsets + circuit breaker
├── virtualservice-canary.yaml            # 90/10 traffic split
├── destinationrule-resilience.yaml       # standalone resilience settings
├── fault-injection-delay.yaml            # deliberately break things, on purpose
├── peerauthentication-strict.yaml        # mTLS everywhere in `demo`
├── authorizationpolicy-default-deny.yaml # zero trust, opened deliberately
└── authorizationpolicy-allow.yaml        # explicit, narrow allow rules
```

Every file is commented with the lesson it comes from and the reasoning
behind each setting — not just the syntax.

## Two things worth knowing before you start

**Istio's sidecar can collide with the strict `securityContext` from the
Kubernetes course.** The init container that redirects traffic into the
sidecar normally needs `NET_ADMIN`/`NET_RAW`, which a namespace enforcing
restricted Pod Security can reject outright. [Lesson 2](02-installing-istio.md)
covers the fix: the Istio CNI plugin, which moves that privilege to a single
node-level DaemonSet instead of granting it per Pod.

**This project is genuinely too small to need Istio in real life.** One API,
Postgres, and Redis do not have enough service-to-service traffic to justify
the operational cost of a mesh — an extra container per Pod, and a whole new
system to operate. We use it here anyway because it is small enough to see
every moving part clearly, not because it is the right call at this scale.
[Lesson 1](01-what-is-a-service-mesh.md) is explicit about when the trade
actually pays off.

## How this connects to the other two courses

| Docker course | Kubernetes course | Istio course |
|---|---|---|
| Container = renting a room | Kubernetes = the building manager | Istio = an assistant at every door |
| `/healthz` vs `/readyz` | Liveness vs readiness probes | Readiness probes vs `outlierDetection` — different signals, same idea |
| — | `Service` (reception desk) | `VirtualService` routes *on top of* the same Service, unchanged |
| — | `Ingress` + a controller | `Gateway` — same job, HTTP-aware routing added |
| — | `NetworkPolicy` (IP/port, default-deny) | `AuthorizationPolicy` (identity/HTTP, default-deny) — one layer up |
| Non-root, distroless image | `securityContext` enforces it | Its init container needs privileges *that* very securityContext can block |
| Never use `:latest` | Rolling updates replace Pods | `VirtualService` weights replace *traffic*, independent of Pod count |
| — | `depends_on` doesn't exist — crash and retry | Retries/timeouts/circuit breaking make "crash and retry" actually safe |

## Clean up

```bash
istioctl uninstall --purge -y
kubectl delete namespace istio-system
kubectl label namespace demo istio-injection-

# or remove everything, cluster included
kind delete cluster --name k8s-course
```
