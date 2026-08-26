# 7. Health Probes

> **Part 2 begins here.** Part 1 got the application running. Part 2 keeps it
> running safely when things go wrong.

## The big idea, in simple words

Our building manager needs to notice when a tenant is **sick**.

But "sick" is not one thing. There are two very different situations, and
mixing them up causes outages:

| Situation | The right reaction |
|---|---|
| The tenant is **unconscious** and will never recover | Replace them (**restart the pod**) |
| The tenant is **busy right now** — on the phone, taking a shower | Stop sending visitors for a moment (**remove from the Service**) |

If you replace a tenant who was only busy, you cause harm for no reason.

Kubernetes gives you three probes for this:

| Probe | Question it asks | If it fails |
|---|---|---|
| **liveness** | "Are you alive?" | **Restart the container** ☠️ |
| **readiness** | "Can you take traffic right now?" | **Remove from the Service** (no restart) |
| **startup** | "Are you still starting up?" | Restart, but only after a long patience |

> **The one sentence to remember:**
> Liveness **kills**. Readiness only **removes from the reception desk's
> list**. Readiness is safe; liveness is dangerous.

## Why `Running` is not the same as `working`

In [lesson 2](02-pods-replicasets-deployments.md) you saw pods with status
`Running`. That only means the process started.

A process can be `Running` and still be completely useless:

- it is still loading a large file into memory
- it is deadlocked and will never respond again
- it lost its database connection
- it is overloaded and every request times out

Without probes, Kubernetes happily sends traffic to all of these. Probes are
how you tell Kubernetes what "actually working" means for *your* application.

## Readiness probe: "should I receive traffic?"

```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 3
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3
```

When this fails, the pod's IP is quietly removed from the Service's endpoint
list ([lesson 3](03-services-and-ingress.md)). The container keeps running.
Nothing is killed. When the probe passes again, the pod is added back.

This is the reception desk saying "room 12 is busy, send visitors to room 14
for now".

**Readiness is the probe you should always have.** It has almost no downside.

Notice which endpoint it uses: `/readyz`. Look at what our Go app does there
in [`handlers.go`](../docker-crash-course/app/internal/api/handlers.go):

```go
// handleReadyz reports readiness: can we actually reach our dependencies.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ...)   // 503 = not ready
		return
	}
	if err := s.Redis.Ping(ctx).Err(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ...)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
```

It **checks the dependencies**. If Postgres is unreachable, this pod says
"do not send me requests" — which is exactly right, because it could not
serve them anyway.

Any HTTP status from 200 to 399 counts as success. 400 and above counts as
failure.

## Liveness probe: "should I be restarted?"

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 15
  timeoutSeconds: 3
  failureThreshold: 3
```

When this fails `failureThreshold` times in a row, Kubernetes **kills the
container and starts it again**.

Use liveness only for one situation: **the process is stuck and only a
restart can fix it.** A deadlock. An unrecoverable internal state.

Look at what `/healthz` does in our app:

```go
// handleHealthz reports liveness only: the process is up and serving.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

It checks **nothing**. It just answers. That looks lazy, but it is exactly
correct, and the next section explains why.

## ⚠️ The most dangerous mistake in Kubernetes

**Never check your database in a liveness probe.**

Imagine you did. Your `/healthz` pings Postgres, and you use it for liveness.

Now Postgres has a 30-second problem:

```
Postgres becomes slow
        │
        ▼
every api pod's liveness probe fails
        │
        ▼
Kubernetes RESTARTS every api pod   ← at the worst possible moment
        │
        ▼
all pods start cold, all reconnect at once
        │
        ▼
the reconnect storm makes Postgres slower
        │
        ▼
liveness fails again → restart again → forever
```

You have turned a **30-second database hiccup** into a **total outage that
cannot recover**. Restarting your API does not fix a sick database. It only
removes the pods that were about to recover on their own.

This is why our app splits the two endpoints:

| Endpoint | Checks dependencies? | Used for | Reasoning |
|---|---|---|---|
| `/healthz` | **No** | liveness | Restarting cannot fix a broken database |
| `/readyz` | **Yes** | readiness | Stop sending traffic, but stay alive and keep retrying |

With this split, a Postgres outage means: all pods go `NotReady`, traffic
stops, **no pod is killed**, and when Postgres recovers, every pod becomes
ready again by itself. No restart storm.

> **Rule:** liveness = "is this process broken?"
> readiness = "is this process, or anything it needs, unavailable right now?"

Many experienced teams use **no liveness probe at all**, on purpose. A missing
liveness probe is much safer than a wrong one.

## Startup probe: patience for slow starters

Some applications need a long time to start — 2 minutes to load a model, warm
a cache, or run migrations.

You have a conflict:

- Liveness must be **fast** to detect a real freeze quickly.
- But a fast liveness probe kills a slow-starting app before it ever
  finishes starting. The app then restarts forever and **never** succeeds.

The startup probe solves it. While it is running, **liveness and readiness are
paused**.

```yaml
startupProbe:
  httpGet:
    path: /healthz
    port: 8080
  failureThreshold: 30      # try up to 30 times...
  periodSeconds: 10         # ...every 10 seconds  = 300s of patience
```

Read it as: "you have 5 minutes to start. After that, normal fast rules
apply."

You only need this if your app takes longer to start than
`initialDelaySeconds` comfortably allows. Our Go API starts in
milliseconds, so it does not need one. It is included in the production
manifest with a short budget purely as an example.

## The probe types

All three probes support the same three mechanisms.

### `httpGet` — the usual choice

```yaml
httpGet:
  path: /readyz
  port: 8080
  httpHeaders:
    - name: Host
      value: api.local
```

### `tcpSocket` — just check the port opens

```yaml
tcpSocket:
  port: 5432
```

Weak, because a port can be open while the app is broken. Use it for things
that do not speak HTTP.

### `exec` — run a command inside the container

```yaml
exec:
  command: ["pg_isready", "-U", "postgres"]
```

Success means exit code 0. This is what our Postgres and Redis manifests use.

> ⚠️ **`exec` does not work on distroless images.** There is no shell and no
> tools ([Docker lesson 6](../docker-crash-course/06-image-security.md)).
> This is exactly the problem we hit when trying to add a healthcheck to the
> Compose file. In Kubernetes it does not matter, because `httpGet` is
> performed **by the kubelet**, from outside the container. The kubelet needs
> no tools inside your image.
>
> This is a real advantage of Kubernetes probes over Docker `HEALTHCHECK`.

## Tuning the numbers

```yaml
readinessProbe:
  httpGet: { path: /readyz, port: 8080 }
  initialDelaySeconds: 3     # wait this long before the FIRST check
  periodSeconds: 5           # then check every 5 seconds
  timeoutSeconds: 3          # a check taking longer than this = failure
  failureThreshold: 3        # this many failures in a row = officially failed
  successThreshold: 1        # this many successes = healthy again
```

How long until Kubernetes reacts?

```
initialDelaySeconds + (periodSeconds × failureThreshold)
       3            +        (5       ×        3      )  = 18 seconds
```

Guidelines that avoid most problems:

| Setting | Readiness | Liveness | Why |
|---|---|---|---|
| `periodSeconds` | 5s | 10–15s | Readiness should react fast; liveness should be slow and calm |
| `failureThreshold` | 3 | 3–5 | Never restart on a single failure |
| `timeoutSeconds` | 2–3s | 3–5s | Must be shorter than `periodSeconds` |
| `initialDelaySeconds` | small | larger | Or use a startup probe instead |

**Make liveness slower and more forgiving than readiness.** Readiness
failing is cheap and reversible. Liveness failing destroys a running process.

## Watching probes work

```bash
kubectl get pods -n demo            # look at the READY column: 1/1 or 0/1
kubectl describe pod <name> -n demo # events show "Readiness probe failed: ..."
kubectl get endpoints api -n demo   # a NotReady pod disappears from here
```

### Try it yourself

Break readiness on purpose and watch the pod leave the Service:

```bash
# 1. watch the endpoints in one terminal
kubectl get endpoints api -n demo -w

# 2. in another terminal, stop Postgres
kubectl scale statefulset/postgres -n demo --replicas=0
```

Within seconds, `/readyz` starts returning 503, and the API pods disappear
from the endpoint list. Check that they are **still running**:

```bash
kubectl get pods -n demo    # STATUS Running, READY 0/1, RESTARTS unchanged
```

That is the whole lesson in one screen. The pods are alive, holding their
connections, waiting. Nothing was killed.

Now bring it back:

```bash
kubectl scale statefulset/postgres -n demo --replicas=1
```

The pods return to the endpoint list by themselves, with **zero restarts**.
If `/healthz` had checked the database, all three would have been killed
instead.

Next: [Resources, limits, and QoS](08-resources-and-qos.md).
