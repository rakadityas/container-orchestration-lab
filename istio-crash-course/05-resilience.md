# 5. Resilience: Retries, Timeouts, and Circuit Breaking

## The big idea, in simple words

Sometimes a call to another apartment fails for a small reason. The line was
busy for one second. Or that tenant has too many visitors right now.

A good assistant does not immediately tell their tenant "it failed". They
think first: try again, hang up if it takes too long, and stop calling an
apartment that keeps failing.

This is exactly what Istio's resilience features do, all configured
centrally and applied by the sidecar — **with no retry loop, timeout timer,
or backoff logic written in your application code.**

## Timeouts: don't wait forever

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: api
  namespace: demo
spec:
  hosts:
    - api
  http:
    - route:
        - destination:
            host: api
            subset: v1
      timeout: 3s
```

If `api` doesn't respond within 3 seconds, the sidecar gives up and returns
an error to the caller — instead of the caller hanging indefinitely.

Without a timeout, one slow service can freeze your whole system. Here is
how:

```
postgres becomes slow
        ↓
every api request waits for it
        ↓
every service that calls api now waits too
        ↓
every service that calls THOSE services waits too
        ↓
nothing works anywhere
```

The waiting spreads backwards, service by service, until everything is
stuck. A timeout stops this. It changes "everything freezes slowly" into
"this one call fails quickly, and the caller decides what to do next".

## Retries: try again, but carefully

```yaml
      retries:
        attempts: 3
        perTryTimeout: 1s
        retryOn: 5xx,reset,connect-failure
```

Read it as: *"try up to 3 times total, give each individual attempt 1
second, and only retry on real server errors or connection failures — never
retry a `4xx`, because that means the *request itself* was wrong, and
retrying it will just fail the same way again."*

### ⚠️ The trap: retries can make an overloaded service worse, not better

This is the most important thing to understand in this whole lesson.

Imagine `api` is genuinely **overloaded** — not broken, just too busy. Every
request into it starts timing out. With retries configured, each failed
request is retried up to 3 times.

```
1 real user request
        │
        ▼
   attempt 1 (fails — the service is too busy)
        │
        ▼
   attempt 2 (fails — the service is now even busier)
        │
        ▼
   attempt 3 (fails — the service is now much worse)
```

**One user request just became three requests to a service that was already
struggling.** Now imagine every caller doing this at the same time. The
number of requests triples exactly when the service can handle the least.

Your retries have turned a slow service into a dead one. This has a name: a
**retry storm**.

Two things must always be true when you configure retries:

1. **`perTryTimeout` must be shorter than the overall `timeout`.** With 3
   attempts at 1s each, your outer `timeout` needs to be at least 3s, or
   the outer timeout cuts the retries off before they finish anyway,
   silently wasting the configuration.
2. **Always use retries together with circuit breaking (explained below).**
   The circuit breaker is what protects a busy service *from* your retries.
   Setting up retries with no circuit breaker does not add safety. It adds
   a way to make an outage worse.

## Circuit breaking: stop calling somebody who keeps failing

A circuit breaker's job: after a destination fails enough times, **stop
sending it traffic for a while**, so it gets a chance to recover instead of
being retried again and again until it dies.

This is configured in the `DestinationRule` you already saw in
[lesson 3](03-traffic-management.md):

```yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: api
  namespace: demo
spec:
  host: api
  trafficPolicy:
    connectionPool:
      tcp:
        maxConnections: 100          # never open more than 100 connections
      http:
        http1MaxPendingRequests: 50  # queue at most 50 waiting requests
        maxRequestsPerConnection: 10
    outlierDetection:
      consecutive5xxErrors: 5        # after 5 errors in a row...
      interval: 30s                  # ...checked over a 30s window...
      baseEjectionTime: 30s          # ...eject the pod for 30 seconds
      maxEjectionPercent: 50         # never eject more than half the pods
```

Two separate mechanisms live inside `trafficPolicy`, and they protect
against different things:

| Setting | Protects against | Building analogy |
|---|---|---|
| `connectionPool` | Sending **too much** traffic to a healthy destination | A rule limiting how many visitors may be in the lobby at once |
| `outlierDetection` | Sending traffic to a destination that's **already failing** | Temporarily stop directing visitors to an apartment whose tenant keeps not answering |

`outlierDetection` is the actual "circuit breaker." Read the settings
above as one sentence: *"if a specific Pod behind this Service returns 5
consecutive server errors, stop sending it traffic for 30 seconds — but
never eject more than half the Pods at once, or we'd have no capacity left
at all."*

```
Pod A: fails, fails, fails, fails, fails   ← 5 in a row
                    │
                    ▼
          Pod A ejected from the pool for 30s
                    │
                    ▼
      traffic goes only to Pods B and C during that time
                    │
                    ▼
         after 30s, Pod A gets a chance again
```

This is genuinely useful during a partial failure: one Pod stuck in a bad
state (for example, holding a dead database connection) no longer damages
the whole Service. Nobody has to wake up at night, and nothing has to be
restarted by hand.

## How this relates to what Kubernetes probes already do

You might be thinking: doesn't
[Kubernetes lesson 7](../kubernetes-crash-course/07-probes.md)'s readiness
probe already remove unhealthy Pods? Yes — but the two mechanisms answer
different questions, on different time scales, using different signals:

| | Kubernetes readiness probe | Istio `outlierDetection` |
|---|---|---|
| Signal | A dedicated `/readyz` endpoint you wrote | **Real production traffic's actual error responses** |
| Checked | Every few seconds, on a schedule | Continuously, on every request |
| Reaction time | Seconds (probe interval) | Immediate — the next request after the threshold |
| What it catches | "I know I'm not ready" (self-reported) | "Something is actually going wrong that the app didn't even know to report" |

They are complementary, not redundant. A readiness probe catches problems
your application knows to check for — like the Postgres/Redis connectivity
check in
[`/readyz`](../docker-crash-course/app/internal/api/handlers.go). Outlier
detection catches problems your application has **no way to know about** —
for example, a Pod that answers its own health check fine but is actually
timing out on this *particular caller's* requests due to a network issue
between just those two Pods.

## Fault injection: testing resilience on purpose

The best way to know your retries and timeouts actually work is to
**deliberately break things in a controlled way**, rather than waiting for
a real incident to be your first test.

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: api
  namespace: demo
spec:
  hosts:
    - api
  http:
    - fault:
        delay:
          percentage:
            value: 50.0
          fixedDelay: 5s
      route:
        - destination:
            host: api
            subset: v1
```

This tells the sidecar: *"for 50% of requests to `api`, artificially add a
5-second delay before actually forwarding the request."* Nothing about
`api`'s own code changes at all — the delay is injected entirely by the
sidecar, purely for testing.

You can also inject outright errors:

```yaml
      fault:
        abort:
          percentage:
            value: 10.0
          httpStatus: 503
```

10% of requests get an immediate `503`, with no real work done at all.

**Use this to answer real questions before an incident forces you to
answer them under pressure:**

- Does my `timeout: 3s` actually trigger when a dependency is slow — or did
  I typo it and it silently does nothing?
- When `api` returns `503`s, does the *caller* of `api` handle that
  gracefully, or does it also fall over?
- Does my outlier detection eject a consistently slow Pod, or does the
  threshold need tuning?

This connects directly to
[Kubernetes lesson 11](../kubernetes-crash-course/11-graceful-shutdown.md)'s
philosophy: don't assume your resilience configuration works — **test it by
deliberately causing the failure**, exactly like that lesson's "run a
rollout and watch for dropped requests" experiment.

## Try it

```bash
kubectl apply -f manifests/destinationrule-resilience.yaml
```

Then inject a delay and watch what happens to a caller with a 3-second
timeout — it should now fail roughly half the time, on purpose:

```bash
kubectl apply -f manifests/fault-injection-delay.yaml
for i in $(seq 1 10); do
  time curl -s -o /dev/null -w "%{http_code}\n" http://api.local/healthz
done
kubectl apply -f manifests/virtualservice-canary.yaml   # restore the canary split
```

`fault-injection-delay.yaml` deliberately shares the name `api` with
`virtualservice-canary.yaml` — applying it **replaces** that object rather
than creating a second, conflicting one (Istio does not merge two separate
`VirtualService`s that both claim the same host through the same Gateway).
Re-applying `virtualservice-canary.yaml` afterward restores the 90/10 split.

Roughly half the requests should take about 5 seconds (or fail, if your
`VirtualService` timeout is shorter than the injected delay) — proof the
resilience settings are actually being enforced, not just sitting in a YAML
file unread.

Next: [Observability](06-observability.md).
