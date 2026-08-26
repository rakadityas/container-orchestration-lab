# 11. Graceful Shutdown

> This is the lesson that fixes the **most common cause of dropped requests
> during a deployment**. Everything in [lesson 10](10-rollouts-and-disruption.md)
> can be configured perfectly and you will still lose requests without this.

## The big idea, in simple words

When a tenant moves out, two things must happen **in the right order**:

1. Reception stops sending visitors to that apartment.
2. The tenant finishes talking to the visitors already inside, then leaves.

If you do it backwards — the tenant leaves while reception still sends
visitors — those visitors arrive at an empty room and get an error.

**That backwards order is the default behaviour in Kubernetes.** You have to
fix it yourself.

## What actually happens when a pod is deleted

Here is the part that surprises everyone. When you delete a pod, Kubernetes
starts **two independent processes at the same time**:

```
                    pod is marked for deletion
                              │
              ┌───────────────┴───────────────┐
              │                               │
              ▼                               ▼
   (A) kubelet sends SIGTERM         (B) endpoints controller removes
       to your container                 the pod from the Service
              │                               │
       your app starts                        ▼
       shutting down                   kube-proxy on EVERY node
              │                        updates its routing rules
              ▼                               │
       app exits                              ▼
                                       traffic finally stops
                                       arriving (1-10 seconds later)
```

These two are **not coordinated**. Nobody waits for the other.

Process (A) is fast — the signal arrives instantly. Process (B) is slow,
because the news must reach every node in the cluster, and each one updates
its own network rules.

So the normal sequence is:

1. Your app receives SIGTERM and shuts down in 50ms. Very well behaved.
2. Two seconds later, a node that has not received the update **sends it a
   request**.
3. Connection refused. The user sees an error.

**Your application did nothing wrong. It shut down too fast.**

## The fix: wait before you shut down

The solution is almost silly: **do nothing for a few seconds.**

A `preStop` hook runs **before** SIGTERM is sent. If it just waits, we give
process (B) time to finish, while the app keeps serving normally.

```yaml
lifecycle:
  preStop:
    sleep:
      seconds: 10        # Kubernetes 1.30+ — no shell needed
```

> **Version note:** the `sleep` hook works by default from Kubernetes
> **1.30**. It exists in 1.29 but is switched off unless an administrator
> turns on the `PodLifecycleSleepAction` feature gate. Check your version
> with `kubectl version`. If you are on something older, read the next
> section for what to do instead.

Now the order is correct:

```
pod marked for deletion
      │
      ├──► endpoints removed → kube-proxy updates → traffic stops   ← finishes during the wait
      │
      └──► preStop: wait 10 seconds  (app still running and serving!)
                    │
                    ▼
            NOW send SIGTERM
                    │
                    ▼
            app finishes in-flight requests and exits cleanly
```

During those 10 seconds the pod still answers requests. It is `Terminating`,
but fully working. By the time it actually stops, nothing is being sent to
it any more.

### If your cluster is older than 1.30

The `sleep` hook type is newer. Before that, everyone used a shell command:

```yaml
lifecycle:
  preStop:
    exec:
      command: ["sh", "-c", "sleep 10"]
```

> ⚠️ **This does not work on our image.** Our distroless image has no shell
> and no `sleep` binary
> ([Docker lesson 6](../docker-crash-course/06-image-security.md)). The hook
> fails silently, and you get no protection while believing you do.
>
> This is a real trade-off of distroless images. The `sleep` hook type in
> Kubernetes 1.30+ solves it properly, because the kubelet performs the wait
> itself, outside your container.

## Your application must also behave

The hook buys time. Your app still has to shut down properly when SIGTERM
finally arrives.

Good news: the Go API from the Docker course **already does this correctly**.
Look at [`main.go`](../docker-crash-course/app/main.go):

```go
// 1. Listen for SIGTERM (and SIGINT).
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

// ... server starts in a goroutine ...

// 2. Block here until a signal arrives.
<-ctx.Done()
log.Println("shutting down")

// 3. Stop accepting NEW connections, but let in-flight requests finish.
//    Give them up to 10 seconds.
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := httpServer.Shutdown(shutdownCtx); err != nil {
	log.Printf("shutdown error: %v", err)
}
```

`http.Server.Shutdown` is the important call. It:

- immediately stops accepting **new** connections
- lets **already running** requests finish
- returns when they are all done, or when the timeout expires

Without this, `main()` would return the instant SIGTERM arrived and every
in-progress request would be cut off mid-response.

### The mistake that breaks all of this

If your container starts your app through a shell, the shell is PID 1 and
**the signal never reaches your program**:

```dockerfile
# ❌ BAD: shell form. `sh` is PID 1 and does not forward SIGTERM.
ENTRYPOINT /app/api

# ✅ GOOD: exec form. Your binary IS PID 1 and receives signals directly.
ENTRYPOINT ["/app/api"]
```

Our [Dockerfile](../docker-crash-course/app/Dockerfile) uses the correct
form. If you ever see "my app ignores SIGTERM and is always killed after 30
seconds", this is the first thing to check.

## `terminationGracePeriodSeconds`: the hard deadline

```yaml
spec:
  terminationGracePeriodSeconds: 45
```

This is the **total** time allowed for everything. When it expires,
Kubernetes sends `SIGKILL`, which cannot be caught, ignored, or delayed. The
process dies instantly, mid-request.

The clock starts when the pod is marked for deletion — **the preStop hook is
inside this budget**, not extra.

So the arithmetic must work out:

```
preStop wait  +  app shutdown time  <  terminationGracePeriodSeconds

      10s     +        10s          <          45s        ✅ safe margin
```

If you set the grace period to 15 seconds with a 10-second preStop hook, your
app gets 5 seconds and then is killed. A silent, confusing failure.

| Setting | Our value | Reason |
|---|---|---|
| `preStop sleep` | 10s | Time for endpoint removal to reach all nodes |
| Go `Shutdown` timeout | 10s | Time for in-flight requests to finish |
| `terminationGracePeriodSeconds` | 45s | Comfortably more than 10 + 10 |

The default grace period is 30 seconds. Raise it for long-running work; keep
it modest for a web API, because rollouts and node drains wait for it.

## The complete, correct shutdown

```yaml
spec:
  terminationGracePeriodSeconds: 45
  containers:
    - name: api
      image: docker-crash-course-api:dev
      lifecycle:
        preStop:
          sleep:
            seconds: 10
      readinessProbe:
        httpGet: { path: /readyz, port: 8080 }
        periodSeconds: 5
```

Full timeline:

```
t=0s    pod marked Terminating
        ├─ removed from Service endpoints (in parallel)
        └─ preStop hook starts waiting
t=0-3s  kube-proxy on all nodes stops routing to this pod
t=0-10s pod still serves any request that already reached it
t=10s   preStop finishes → SIGTERM sent
        Go app calls httpServer.Shutdown()
t=10-12s in-flight requests finish, app exits cleanly
t=45s   (SIGKILL deadline — never reached)
```

**Zero dropped requests.**

## Testing it

Send continuous traffic while triggering a rollout:

```bash
# terminal 1: constant requests, printing any failure
kubectl run curl-loop --rm -it --restart=Never -n demo --image=busybox -- \
  sh -c 'while true; do wget -q -O- http://api/healthz >/dev/null || echo "FAILED $(date)"; sleep 0.1; done'

# terminal 2: force a full rollout
kubectl rollout restart deploy/api -n demo
```

With graceful shutdown configured, you should see **no** `FAILED` lines.

Remove the `preStop` hook, apply, and run the test again. Now you will
usually see a burst of failures at the moment each pod terminates. That
difference is the entire lesson.

## Common problems

| Symptom | Cause |
|---|---|
| Errors during every deploy | No `preStop` hook — the endpoint race |
| Pods always take exactly 30s to die | App ignores SIGTERM; killed at the deadline |
| Signal never arrives | Shell-form `ENTRYPOINT`, or a shell wrapper as PID 1 |
| Requests cut off mid-response | No `Shutdown()` call; the app exits immediately |
| preStop seems ignored | `exec` + `sleep` on a distroless image (no binary) |
| Killed before finishing | `terminationGracePeriodSeconds` < preStop + shutdown |

Next: [StatefulSets](12-statefulsets.md).
