# 6. Observability: Seeing What the Mesh Sees

## The big idea, in simple words

Every assistant standing at every door is already watching every call: who
called, how long it took, and whether it succeeded. A service mesh's last
superpower is simply **writing all of that down and showing it to you** —
for every service, in a consistent format, without asking a single
application to add any logging or metrics code.

This is a direct, practical payoff of the sidecar model from
[lesson 1](01-what-is-a-service-mesh.md): because *every* call already
passes through Envoy, Envoy can measure *every* call, for free, regardless
of what language the service is written in or whether its author remembered
to add instrumentation.

## The four tools, and what each one answers

| Tool | Question it answers | Analogy |
|---|---|---|
| **Prometheus** | "How many requests, how fast, how many errors — over time?" | The building's utility meters |
| **Grafana** | "Show me those numbers as a chart" | The dashboard on the wall of the manager's office |
| **Kiali** | "What does the whole building's call graph actually look like?" | A live wiring diagram of every phone line |
| **Jaeger** | "This one specific request was slow — which room did it get stuck in?" | Following one visitor's exact path, room by room, with a stopwatch |

You do not need to memorize which does what right now — the labs below
make the difference concrete.

## Installing the observability add-ons

These are separate from the core Istio install from
[lesson 2](02-installing-istio.md) — they are optional, and on a laptop
worth installing deliberately, one at a time, so you know what each is
costing you in resources.

```bash
cd ../istio-crash-course

kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/prometheus.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/grafana.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/kiali.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/jaeger.yaml

kubectl wait --for=condition=Ready pods --all -n istio-system --timeout=180s
```

> These manifests are maintained in the Istio project's own repository and
> pinned here to release `1.23` for reproducibility — check
> `istioctl version` and adjust the branch if you installed a different
> Istio version.

Generate a little traffic first so the dashboards have something to show:

```bash
for i in $(seq 1 50); do curl -s http://api.local/items > /dev/null; done
```

## Kiali: the live wiring diagram

```bash
istioctl dashboard kiali
```

This opens a browser tab and forwards a local port automatically — no need
to remember a `kubectl port-forward` command yourself. In the **Graph**
view, select the `demo` namespace.

You will see something like this, drawn live from real traffic:

```
   ingress-gateway ──▶ api ──▶ postgres
                        │
                        └────▶ redis
```

Every arrow is a **real, currently-flowing connection** the mesh observed —
not something anyone had to configure or diagram by hand. If you applied
the traffic-splitting `VirtualService` from
[lesson 3](03-traffic-management.md), you will see the graph split into two
separate arrows toward `api` `v1` and `v2`, each labeled with its actual
percentage of traffic.

This is the fastest way to answer "wait, what actually talks to what in
this system?" — a question that gets harder to answer by reading code alone
as a system grows past a handful of services.

## Grafana: the metrics dashboard

```bash
istioctl dashboard grafana
```

Open the **Istio Service Dashboard**, and select `api.demo.svc.cluster.local`
as the service. You'll see the same four numbers, called the **golden
signals**, for every service in the mesh, with zero application code:

| Signal | Question |
|---|---|
| **Request rate** | How many requests per second? |
| **Error rate** | What fraction are failing (4xx/5xx)? |
| **Duration (latency)** | How long do requests take — especially p99, the slowest 1%? |
| **Saturation** | How close is this service to its resource limits? |

Compare this to
[Kubernetes lesson 8](../kubernetes-crash-course/08-resources-and-qos.md)'s
`kubectl top pods` — that told you raw CPU and memory usage. This tells you
**what your users are actually experiencing**: a Pod can have plenty of
free CPU and still be returning errors, and only the golden signals surface
that.

## Jaeger: tracing one request across services

This is the tool for the question the other three cannot answer: *"a single
request was slow — where, specifically, did the time go?"*

```bash
istioctl dashboard jaeger
```

In a system with only one service, tracing feels unnecessary. It becomes
essential the moment a single user request touches multiple services — a
request to `api` that calls `postgres` and `redis` already has three hops,
and in a larger system that chain might be ten services long. Aggregate
metrics tell you the *whole call* took 800ms; only a trace tells you it was
because hop 6 alone took 750ms of it.

```
Trace for one request to GET /items
├─ ingress-gateway          2ms
├─ api                      780ms  (total)
│   ├─ postgres query       750ms  ← the actual problem
│   └─ redis call           5ms
```

### ⚠️ One real limitation, honestly stated

Tracing has one gap that surprises people. **Envoy can measure the time
between a request arriving at your Pod and a response leaving it. It cannot
see what happens *inside* your application code.**

So if `api`'s Go code spent 700ms doing internal work before it ever called
Postgres, the trace shows one big block of 700ms for `api`. It cannot tell
you what the Go code was doing during that time.

Getting a full breakdown *inside* the application requires the app itself
to participate — propagating trace headers and creating its own spans,
typically via OpenTelemetry. That is real application code, deliberately
outside the "zero code changes" promise of the mesh — a limit worth knowing
rather than discovering by surprise.

## Prometheus: where all of it actually comes from

Kiali, Grafana, and (for metrics) Jaeger are all just **different ways of
looking at data that Prometheus already collected**. Envoy exposes metrics
on every sidecar; Prometheus scrapes them on a schedule and stores the
history.

```bash
istioctl dashboard prometheus
```

You rarely query Prometheus directly once Grafana is set up, but it's worth
knowing it's the actual database underneath — if Grafana's dashboards ever
look empty, the first thing to check is whether Prometheus itself is
`Running` and actually scraping:

```bash
kubectl get pods -n istio-system -l app=prometheus
```

## The whole picture

```
        every sidecar continuously measures every call
                          │
                          ▼
                    Prometheus
              (scrapes and stores metrics)
              /            |              \
             ▼             ▼               ▼
         Grafana         Kiali          Jaeger
      (charts over    (live call      (per-request
       time)           graph)          timeline)
```

One thing — the sidecar standing between every call — powers all four tools.

This is what you get in return for the cost described in
[lesson 1](01-what-is-a-service-mesh.md). You run one extra container in
every Pod. In exchange, every service is measured the same way, with no
code written in any of them.

## Clean up

```bash
kubectl delete -n istio-system -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/kiali.yaml
kubectl delete -n istio-system -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/jaeger.yaml
kubectl delete -n istio-system -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/grafana.yaml
kubectl delete -n istio-system -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/prometheus.yaml
```

Next: [Lab: full mesh setup and canary release](07-lab-full-mesh-setup.md).
