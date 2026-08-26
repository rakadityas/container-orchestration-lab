# 1. What Is a Service Mesh, and Why Istio?

## The big idea, in simple words

Remember the story so far. In the
[Docker course](../docker-crash-course/01-containers-vs-vms.md), a container
was **renting one room**. In the
[Kubernetes course](../kubernetes-crash-course/01-why-kubernetes.md),
Kubernetes became **the building manager** who keeps apartments filled and
finds the right room for every visitor.

Now imagine the building has grown to 50 apartments, and tenants call each
other constantly — the API calls the database, the checkout service calls
the payment service, the payment service calls a fraud-check service. Every
one of those calls needs the same handling:

- Prove who is calling (so a stranger cannot just walk in)
- Encrypt the conversation (so nobody on the phone line can listen in)
- Retry if the line is busy
- Hang up and try someone else if a tenant never answers
- Keep a record of every call, for the building's records

Today, **every tenant (every application) has to implement all of this
themselves**, in whatever language they happen to be written in. That is a
lot of duplicated, easy-to-get-wrong work, repeated in every single service.

**A service mesh gives every apartment a personal assistant. The assistant
stands at the door and handles all of this for the tenant.**

The tenant (your application code) simply says "call apartment 12". The
assistant does everything else: makes the call safely, checks the ID of the
person who answers, tries again if the line is busy, and writes down what
happened.

```
                    WITHOUT a mesh                          WITH a mesh

  ┌─────────────┐                          ┌─────────────┐
  │  Service A  │  handles retries,        │  Service A  │  "call B" — done.
  │  (app code) │  TLS, timeouts,          │  (app code) │
  │             │  itself, in ITS          └──────┬──────┘
  │             │  OWN language                   │
  └──────┬──────┘                          ┌──────▼──────┐
         │  direct call                    │  Assistant   │ ← handles retries,
         │                                 │ (sidecar)    │   TLS, timeouts
         ▼                                 └──────┬──────┘
  ┌─────────────┐                                 │ encrypted, monitored
  │  Service B  │                          ┌──────▼──────┐
  │  (app code, │                          │  Assistant   │
  │  own retry  │                          └──────┬──────┘
  │  logic)     │                                 │
  └─────────────┘                          ┌──────▼──────┐
                                            │  Service B  │  "someone's calling" — done.
                                            │  (app code) │
                                            └─────────────┘
```

**Istio is the most widely used service mesh for Kubernetes.** It is not a
replacement for Kubernetes — it is an add-on that works *alongside* it.

## The sidecar: an assistant that moves in with the tenant

This is the core mechanic, and it connects directly to something you
already know.

Remember from [Kubernetes lesson 2](../kubernetes-crash-course/02-pods-replicasets-deployments.md):
a Pod can hold more than one container, and containers in the same Pod
share the network. A helper container living alongside the main one is
called a **sidecar**.

Istio's assistant is exactly that: a **sidecar container** named
`istio-proxy`, injected into your Pod next to your application container.
It runs a program called **Envoy**, a fast network proxy.

```
┌─────────────────────────────────────┐
│  Pod                                 │
│                                       │
│   ┌───────────────┐  ┌─────────────┐ │
│   │  api container │  │ istio-proxy │ │  ← the "assistant"
│   │  (your Go app) │◄─┤  (Envoy)    │ │
│   └───────────────┘  └──────┬──────┘ │
│                              │        │
└──────────────────────────────┼────────┘
                                │  every network call in and out
                                │  of this pod passes through here
                                ▼
                    the rest of the mesh
```

Once injected, **every network call in or out of the Pod is quietly
redirected through this sidecar**, using `iptables` rules set up by an init
container when the Pod starts. Your application does not know this is
happening. It still just makes a normal HTTP call to `http://api`, exactly
as it did in the Kubernetes course. The sidecar intercepts it, applies
whatever rules the mesh owner configured, and forwards it on.

**This is the single biggest thing to understand about Istio: your
application code never changes.** The Go API from the Docker course needs
zero code edits to run inside the mesh. That is deliberate — it is what
"the assistant handles it, not the tenant" means in practice.

## Data plane vs control plane

The assistants need to be trained, and they all need the same rulebook.
That is the second half of Istio.

| Part | Building analogy | What it actually is |
|---|---|---|
| **Data plane** | Every apartment's personal assistant | The `istio-proxy` sidecars — one per Pod, actually handling traffic |
| **Control plane** | The head office that trains every assistant | `istiod` — one component that watches your config and pushes rules to every sidecar |

```
                    ┌──────────────────┐
                    │      istiod       │   "head office" — reads your
                    │  (control plane)  │    VirtualService/Gateway/etc.
                    └─────────┬─────────┘    YAML and configures every
                               │              sidecar to match
             ┌─────────────────┼─────────────────┐
             ▼                 ▼                 ▼
      ┌────────────┐   ┌────────────┐    ┌────────────┐
      │ Pod: api    │   │ Pod: web   │    │ Pod: pay   │
      │ ┌────┐┌───┐ │   │┌────┐┌───┐│    │┌────┐┌───┐│
      │ │app ││sc │ │   ││app ││sc ││    ││app ││sc ││   sc = sidecar
      │ └────┘└───┘ │   │└────┘└───┘│    │└────┘└───┘│
      └────────────┘   └────────────┘    └────────────┘
```

You never talk to a sidecar directly. You write YAML (`VirtualService`,
`DestinationRule`, and so on — covered in
[lesson 3](03-traffic-management.md)), `istiod` reads it, and it pushes the
resulting configuration out to every relevant sidecar automatically. This is
the exact same **declarative, reconciling** model from
[Kubernetes lesson 1](../kubernetes-crash-course/01-why-kubernetes.md) — you
describe the desired state, and a controller keeps reality matching it.

## What a mesh actually buys you

Four things, and each gets its own lesson:

| Capability | In one line | Lesson |
|---|---|---|
| **Traffic management** | Route, split, and shape traffic without touching app code | [3](03-traffic-management.md) |
| **Security (mTLS)** | Every call encrypted and identity-checked automatically | [4](04-security-mtls.md) |
| **Resilience** | Retries, timeouts, and circuit breaking, configured centrally | [5](05-resilience.md) |
| **Observability** | Every call measured and traced, with zero app code changes | [6](06-observability.md) |

## Istio vs Kubernetes NetworkPolicy — these are not the same tool

You already met `NetworkPolicy` in
[Kubernetes lesson 5](../kubernetes-crash-course/05-rbac-and-networkpolicy.md).
It answers *"which pod may open a connection to which pod"* — a yes/no
decision made at the network layer (IP addresses and ports), enforced by the
CNI plugin (Calico, in that course).

Istio works one layer up, at the **application layer** — it understands
HTTP, paths, headers, and versions, not just IPs and ports.

| | NetworkPolicy | Istio |
|---|---|---|
| Layer | Network (L3/L4) — IP, port | Application (L7) — HTTP path, header, version |
| Question it answers | "May A connect to B at all?" | "Route 10% of A's traffic for `/v2` to version 2 of B, retry twice, and prove A's identity cryptographically" |
| Enforced by | The CNI plugin (Calico) | The sidecar proxy (Envoy) |
| Encrypts traffic? | No | Yes (mTLS) |

**They are not competitors — they are complementary layers.** A well-secured
cluster usually uses both. NetworkPolicy is the simple outer wall ("these
namespaces may never talk to those"). Istio handles the detailed rules
inside that wall (identity, encryption, retries, traffic splitting).

## Is Istio always the right choice?

No. Be honest about the cost before adopting it.

**The cost:**

- **Every Pod gets an extra container.** More memory, more CPU, and another
  moving part to understand when something breaks.
- **Debugging gets one layer deeper.** A failed call could be your app, or
  it could be the sidecar's retry/timeout/circuit-breaker configuration.
- **Real operational complexity.** Certificate rotation, control plane
  upgrades, and sidecar injection all need to be understood by your team.

**When it is worth the cost:**

- You have **many services calling each other** and are duplicating retry,
  timeout, and TLS logic across languages and teams.
- You need **mTLS everywhere** for compliance reasons, without rewriting
  every application.
- You want to **run a canary release** (send 5% of traffic to a new version)
  without building that logic into the application itself.
- You need **consistent observability** across services written in
  different languages, without adding a tracing library to each one.

**When to skip it:**

- A handful of services. Plain Kubernetes `Service` objects and application-
  level retry logic are simpler and easier to debug.
- A learning project or an early-stage product where operational simplicity
  matters more than the traffic-management features.

Our practice project — one Go API, Postgres, Redis — is **too small to need
Istio in real life**. We use it here because it is small enough to see every
part clearly. Think of this course as "here is how the machine works", not
"every project needs this machine".

## Sidecar mode and the newer "ambient" mode

This course uses Istio's original **sidecar mode**: one `istio-proxy`
container added to every Pod, as described above. It is the most common
mode, it has the most documentation, and it has been used the longest. So it
is the right one to learn first.

Istio also has a newer mode called **ambient mesh**. It removes the sidecar
from each Pod. Instead, it runs one shared proxy on each *node*. This uses
less memory per Pod, and you do not need to restart Pods to turn it on. But
it is newer and has less documentation.

| | Sidecar mode (this course) | Ambient mode |
|---|---|---|
| Proxy per | Pod | Node |
| Pod restart needed to enable? | Yes | No |
| Resource overhead per pod | Higher | Lower |
| Maturity | Well-established | Newer, growing fast |

Learn sidecar mode first. The concepts (VirtualService, mTLS, retries) are
identical in ambient mode — only *where the proxy runs* changes.

Next: [Installing Istio](02-installing-istio.md).
