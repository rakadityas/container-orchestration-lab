# 3. Traffic Management: Gateway, VirtualService, DestinationRule

## The big idea, in simple words

Three objects, three different jobs. People mix them up constantly, so keep
them separate using the building story:

| Object | Its one job | In the building story |
|---|---|---|
| **Gateway** | Open a door from the street | The building's actual front door |
| **VirtualService** | Decide *which room* a call goes to | The receptionist's routing rules |
| **DestinationRule** | Decide *how* to treat a room once chosen | House rules for that specific room (how many visitors at once, which sub-rooms exist) |

```
       internet
           │
           ▼
    ┌─────────────┐
    │   Gateway    │   "open port 80/443, accept traffic for api.local"
    └──────┬───────┘
           │
           ▼
    ┌─────────────┐
    │VirtualService│   "requests to /  → 90% v1, 10% v2"
    └──────┬───────┘
           │
           ▼
    ┌─────────────┐
    │DestinationRule│  "v1 and v2 are subsets of the api Service;
    └──────┬───────┘   limit each to 100 connections at a time"
           │
     ┌─────┴─────┐
     ▼           ▼
  api v1       api v2
```

This is one layer **above** the plain Kubernetes `Service` and `Ingress`
objects from
[Kubernetes lesson 3](../kubernetes-crash-course/03-services-and-ingress.md).
You still have those — Istio's objects sit on top and add HTTP-aware
routing that plain Kubernetes cannot do.

## Gateway: opening the front door

```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: api-gateway
  namespace: demo
spec:
  selector:
    istio: ingressgateway     # use Istio's own ingress gateway pod
  servers:
    - port:
        number: 80
        name: http
        protocol: HTTP
      hosts:
        - "api.local"
```

Compare this to the Kubernetes `Ingress` object from
[Kubernetes lesson 3](../kubernetes-crash-course/03-services-and-ingress.md).
They solve the same *problem* — a front door for traffic from outside — but
a Kubernetes `Ingress` only opens the door. An Istio `Gateway` also only
opens the door. **Neither one, by itself, decides where traffic goes once
it's inside.** That is always a separate object: `VirtualService` here,
just as a plain `Ingress` needed backend rules.

`selector: istio: ingressgateway` points at the ingress gateway Pod we
installed in [lesson 2](02-installing-istio.md) — Istio's equivalent of the
nginx controller from the Kubernetes course.

## VirtualService: the routing rules

This is where the real decisions live. A `VirtualService` matches incoming
requests (by host, path, headers) and decides where they go.

### The simplest case: just route everything to one place

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: api
  namespace: demo
spec:
  hosts:
    - "api.local"
  gateways:
    - api-gateway            # apply these rules to traffic through our Gateway
  http:
    - route:
        - destination:
            host: api        # the plain Kubernetes Service name
            subset: v1
```

Read this as one sentence: *"traffic for `api.local` arriving through
`api-gateway` goes to the `v1` subset of the `api` Service."*

Notice `host: api` — this is the **same Kubernetes Service name** you
already know from
[Kubernetes lesson 3](../kubernetes-crash-course/03-services-and-ingress.md).
Istio does not replace Kubernetes Services. It reads them and adds routing
logic on top.

`subset: v1` refers to a label group defined in a `DestinationRule` — covered
next.

### Splitting traffic between two versions (canary)

This is the single most useful thing a service mesh gives you that plain
Kubernetes cannot: shifting a *percentage* of live traffic to a new version,
with no application code changes.

```yaml
  http:
    - route:
        - destination:
            host: api
            subset: v1
          weight: 90
        - destination:
            host: api
            subset: v2
          weight: 10
```

`weight` values must add up to 100. Here, 9 out of 10 requests go to the
stable version, 1 in 10 goes to the new one. If `v2` starts erroring, you
edit one number back to `weight: 0` — no rollback, no redeploy, just a
config change that `istiod` pushes to every sidecar within seconds.

Compare this to the rolling update you did in
[Kubernetes lesson 10](../kubernetes-crash-course/10-rollouts-and-disruption.md).
A Kubernetes rolling update replaces Pods gradually, but it has **no concept
of a percentage of traffic** — as soon as a new Pod is `Ready`, it receives
its full, equal share of requests. Istio traffic splitting is precise and
independent of how many Pods of each version actually exist.

### Routing by header — the safest kind of canary

Splitting by weight sends *some real users* to the new version, at random.
Often you want the new version to be reachable **only** by your own team
first:

```yaml
  http:
    - match:
        - headers:
            x-canary:
              exact: "true"
      route:
        - destination:
            host: api
            subset: v2
    - route:                     # everyone else — the default, listed LAST
        - destination:
            host: api
            subset: v1
```

**Order matters.** Istio checks `http` entries top to bottom and uses the
**first match**. The catch-all route with no `match:` at all must go
**last**. If you put it first, it would catch every request before the
header rule is ever checked.

Now your team tests `v2` in production by sending one header:

```bash
curl -H "x-canary: true" http://api.local/healthz
```

Every other user is completely unaffected and never reaches `v2`.

## DestinationRule: defining subsets and connection behaviour

A `VirtualService` can only route to a `subset` if a `DestinationRule` has
defined what that subset *means*.

```yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: api
  namespace: demo
spec:
  host: api                  # the Kubernetes Service this rule applies to
  subsets:
    - name: v1
      labels:
        version: v1          # pods with this label ARE the v1 subset
    - name: v2
      labels:
        version: v2
  trafficPolicy:
    connectionPool:
      tcp:
        maxConnections: 100
      http:
        http1MaxPendingRequests: 50
```

Read the `subsets` block exactly like a `Service` selector from
[Kubernetes lesson 3](../kubernetes-crash-course/03-services-and-ingress.md):
it groups Pods by **label**, not by name. For this to work, your Deployments
must actually carry a `version` label:

```yaml
# api-v1 Deployment
template:
  metadata:
    labels:
      app: api
      version: v1     # <- this is what makes it the "v1" subset
```

```yaml
# api-v2 Deployment — same app label, different version
template:
  metadata:
    labels:
      app: api
      version: v2
```

Both Deployments sit behind the **same plain Kubernetes Service**
(`selector: app: api`, with no `version` in its own selector) — Kubernetes
does not know or care about the split. Istio's `DestinationRule` is what
carves that one Service into named subsets.

```
         Kubernetes Service "api"
              (selector: app=api)
                       │
        ┌──────────────┴──────────────┐
        ▼                              ▼
  pods: app=api,version=v1      pods: app=api,version=v2
        │                              │
   DestinationRule subset "v1"    DestinationRule subset "v2"
```

`trafficPolicy.connectionPool` is a house rule applied regardless of which
subset is chosen — "no more than 100 simultaneous connections to this
Service." This is the beginning of resilience configuration, which
[lesson 5](05-resilience.md) covers in full.

## Putting the three together

```
Gateway "api-gateway"
    accepts traffic for host api.local on port 80
           │
           ▼
VirtualService "api"
    for host api.local, route:
      90% → subset v1
      10% → subset v2
           │
           ▼
DestinationRule "api"
    subset v1 = pods labelled version=v1
    subset v2 = pods labelled version=v2
    (+ connection pool limits, applied to both)
```

Notice that only the **Gateway** deals with traffic from *outside* the
cluster. `VirtualService` and `DestinationRule` work for **any** traffic —
including calls from one internal service to another, with no Gateway
involved at all. A `VirtualService` with no `gateways:` field applies to
**every** sidecar in the mesh calling that host. This is how you would
canary an internal service (like a payments service another internal
service calls) with no public entry point in sight.

## Try it: split traffic to a second version

[`manifests/api-v2-deployment.yaml`](manifests/api-v2-deployment.yaml)
deploys a second copy of the exact same image, labelled `version: v2`
purely so you can see the split happen — in a real canary the image tag
would actually differ.

```bash
kubectl apply -f manifests/api-v2-deployment.yaml
kubectl apply -f manifests/gateway.yaml
kubectl apply -f manifests/destinationrule.yaml
kubectl apply -f manifests/virtualservice-canary.yaml
```

Send a burst of requests and watch which pod answers each one (our API has
no version marker in its response body, so we read it from the pod logs
instead):

```bash
for i in $(seq 1 20); do curl -s http://api.local/healthz > /dev/null; done
kubectl logs -n demo -l app=api,version=v1 -c api --tail=20 | grep -c '"GET /healthz"'
kubectl logs -n demo -l app=api,version=v2 -c api --tail=20 | grep -c '"GET /healthz"'
```

Roughly 18 and 2 — a 90/10 split, exactly as configured. Change the
`weight` values in
[`manifests/virtualservice-canary.yaml`](manifests/virtualservice-canary.yaml),
re-`apply`, and repeat the test. The shift takes effect within seconds, with
**no pod restart at all** — a sharp contrast to
[Kubernetes lesson 4](../kubernetes-crash-course/04-config-and-secrets.md),
where changing a ConfigMap needed a manual rollout restart to take effect.

Next: [Security and mTLS](04-security-mtls.md).
