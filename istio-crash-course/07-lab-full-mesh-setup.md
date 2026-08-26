# 7. Lab: Full Mesh Setup, Canary Release, and Verification

This lesson ties every previous one together into one continuous exercise:
install the mesh, inject sidecars, split traffic between two versions, lock
down security, and prove — not assume — that each piece actually works.

> **Before you start:** this lab assumes the Kubernetes course's `demo`
> namespace (api + postgres + redis) is already running. If not, see
> [lesson 2](02-installing-istio.md)'s setup block first.

## Step 0: label the api Deployment with a version

The base manifests from the Kubernetes course don't carry a `version`
label, which the `DestinationRule` subsets in
[lesson 3](03-traffic-management.md) need to tell `v1` and `v2` apart:

```bash
kubectl patch deployment api -n demo --type=merge -p \
  '{"spec":{"template":{"metadata":{"labels":{"version":"v1"}}}}}'
kubectl rollout status deploy/api -n demo
```

## Step 1: install Istio

```bash
cd istio-crash-course
istioctl install -f manifests/istio-install.yaml -y
istioctl verify-install
kubectl get pods -n istio-system
```

Both `istiod` and `istio-ingressgateway` should be `Running` before you
continue.

## Step 2: enable sidecar injection and restart

```bash
kubectl label namespace demo istio-injection=enabled
kubectl rollout restart deployment -n demo
kubectl rollout restart statefulset -n demo
kubectl get pods -n demo
```

Every Pod should now read `2/2`. If any still say `1/1`, they haven't
restarted yet — wait, or check for the security collision described in
[lesson 2](02-installing-istio.md).

## Step 3: replace the plain Ingress with Istio's Gateway

The Kubernetes course's plain `Ingress` (in
`../kubernetes-crash-course/manifests/base/05-api.yaml`) and Istio's
`Gateway` both claim to be the front door for `api.local`. Remove the old
one so there's no ambiguity about which is actually serving traffic:

```bash
kubectl delete ingress api -n demo
kubectl apply -f manifests/gateway.yaml
```

If you're on a fresh kind cluster with **no** nginx ingress controller
installed at all, this step is a no-op — skip the `delete` and just apply
the `Gateway`.

### Point your laptop at the Istio gateway, not port 80

The Kubernetes course's `kind-config.yaml` mapped `hostPort: 80` straight to
the **nginx** ingress controller. Istio's ingress gateway is a different
Service, reached through a `NodePort` (see
[lesson 2](02-installing-istio.md)'s install profile). Find its port and use
it directly:

```bash
kubectl get svc istio-ingressgateway -n istio-system
# note the NodePort next to 80:XXXXX/TCP

curl -H "Host: api.local" http://localhost:<that-nodeport>/healthz
```

Using an explicit `Host:` header avoids needing another `/etc/hosts` edit
for this lab — but add one if you'd rather use `http://api.local:<port>/`
directly, matching how you tested things in the Kubernetes course.

## Step 4: deploy the v2 subset and split traffic

```bash
kubectl apply -f manifests/api-v2-deployment.yaml
kubectl apply -f manifests/destinationrule.yaml
kubectl apply -f manifests/virtualservice-canary.yaml
kubectl get pods -n demo -l app=api --show-labels
```

You should see pods with both `version=v1` and `version=v2`.

Send traffic and confirm the split, exactly as in
[lesson 3](03-traffic-management.md):

```bash
NODEPORT=$(kubectl get svc istio-ingressgateway -n istio-system -o jsonpath='{.spec.ports[?(@.port==80)].nodePort}')
for i in $(seq 1 20); do
  curl -s -H "Host: api.local" http://localhost:$NODEPORT/healthz > /dev/null
done
kubectl logs -n demo -l app=api,version=v1 -c api --tail=30 | grep -c '"GET /healthz"'
kubectl logs -n demo -l app=api,version=v2 -c api --tail=30 | grep -c '"GET /healthz"'
```

Roughly 18 and 2. Try shifting it further — edit the `weight` values in
`manifests/virtualservice-canary.yaml` to `50`/`50`, re-`apply`, and repeat.
No pod restart, no deploy — the shift is live within seconds.

## Step 5: watch the mesh in Kiali

```bash
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/prometheus.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/addons/kiali.yaml
kubectl wait --for=condition=Ready pods -l app=kiali -n istio-system --timeout=120s

istioctl dashboard kiali
```

Generate a bit more traffic and watch the graph split live between the
`v1` and `v2` subsets, matching your configured weights — the visual
confirmation that [lesson 6](06-observability.md) promised.

## Step 6: lock down security

Turn on mTLS, then flip from open to default-deny to explicit allow — the
same three-step progression from
[Kubernetes lesson 6](../kubernetes-crash-course/06-deploy-to-kind.md)'s
NetworkPolicy exercise, one layer deeper:

```bash
# 1. mTLS everywhere in the namespace
kubectl apply -f manifests/peerauthentication-strict.yaml
istioctl authn tls-check $(kubectl get pod -n demo -l app=api,version=v1 -o jsonpath='{.items[0].metadata.name}').demo postgres.demo.svc.cluster.local

# 2. default-deny — confirm the app breaks
kubectl apply -f manifests/authorizationpolicy-default-deny.yaml
curl -s -o /dev/null -w "%{http_code}\n" -H "Host: api.local" http://localhost:$NODEPORT/healthz
# expect a failure or a non-200 — this is CORRECT, same as the Kubernetes course

# 3. explicit allow rules — confirm it works again
kubectl apply -f manifests/authorizationpolicy-allow.yaml
curl -s -o /dev/null -w "%{http_code}\n" -H "Host: api.local" http://localhost:$NODEPORT/healthz
# expect 200
```

### Prove an unauthorized pod really is blocked, not just unlucky

```bash
kubectl run attacker --rm -it --restart=Never -n demo --image=curlimages/curl -- \
  curl -s -o /dev/null -w "%{http_code}\n" --max-time 3 http://postgres:5432
```

A pod with no ServiceAccount matching the allow rule's `principals:` should
fail to connect, even though it is inside the same namespace and even
though `NetworkPolicy` (if you still have it from the Kubernetes course)
would already block it too — this proves the **second, independent** layer
is also doing its job.

## Step 7: prove the resilience settings really work

```bash
kubectl apply -f manifests/fault-injection-delay.yaml   # replaces the canary VirtualService, see lesson 5

time curl -s -o /dev/null -w "%{http_code}\n" -H "Host: api.local" http://localhost:$NODEPORT/healthz
time curl -s -o /dev/null -w "%{http_code}\n" -H "Host: api.local" http://localhost:$NODEPORT/healthz
time curl -s -o /dev/null -w "%{http_code}\n" -H "Host: api.local" http://localhost:$NODEPORT/healthz

kubectl apply -f manifests/virtualservice-canary.yaml   # restore the real routing
```

Roughly half the calls should take about 5 seconds (or fail, since the
`VirtualService`'s `timeout: 3s` is shorter than the injected 5s delay) —
this is the intended lesson: **your `timeout` setting is what causes the
failure here, not a bug.** A caller configured to retry on `5xx` would
retry this exact scenario — reinforcing why
[lesson 5](05-resilience.md)'s warning about retries against real overload
matters.

## Troubleshooting

Work through these in order — same discipline as both earlier courses.

```bash
kubectl get pods -n demo                         # 1. are sidecars present? (2/2)
kubectl get pods -n istio-system                 # 2. is istiod/gateway healthy?
istioctl analyze -n demo                          # 3. Istio's own config linter
kubectl logs <pod> -n demo -c istio-proxy --tail=50   # 4. did the sidecar even see the request?
istioctl proxy-status                              # 5. is every sidecar in sync with istiod?
```

Run `istioctl analyze` whenever something in this lab does not work. It
checks your configuration and reports the most common mistakes directly —
for example, a `VirtualService` pointing at a subset that no
`DestinationRule` defines, or a `Gateway` with no matching
`VirtualService`. Try it before you start reading through logs.

| Symptom | Likely cause |
|---|---|
| Pods stuck `1/1`, sidecar never appears | Namespace label missing, or Pods not restarted — [lesson 2](02-installing-istio.md) |
| `503` from the gateway | No `VirtualService` matches the host, or it points at an undefined `subset` |
| Traffic all goes to `v1`, none to `v2` | `version: v2` label missing on the Deployment's Pods |
| Everything blocked after `PeerAuthentication: STRICT` | A Pod without a sidecar is still trying to call in — check it's `2/2` first |
| Everything blocked after default-deny `AuthorizationPolicy` | Expected — apply the allow policies next |
| `istioctl dashboard` hangs | The addon Pod isn't `Ready` yet — check `kubectl get pods -n istio-system` |

## Clean up

```bash
kubectl delete -f manifests/authorizationpolicy-allow.yaml
kubectl delete -f manifests/authorizationpolicy-default-deny.yaml
kubectl delete -f manifests/peerauthentication-strict.yaml
kubectl delete -f manifests/virtualservice-canary.yaml
kubectl delete -f manifests/destinationrule.yaml
kubectl delete -f manifests/gateway.yaml
kubectl delete -f manifests/api-v2-deployment.yaml

kubectl label namespace demo istio-injection-
kubectl rollout restart deployment -n demo
kubectl rollout restart statefulset -n demo

istioctl uninstall --purge -y
kubectl delete namespace istio-system
```

Or remove everything, including the cluster itself:

```bash
kind delete cluster --name k8s-course
```

## What you built

You took the same three services from the Docker and Kubernetes courses and
added, with **zero application code changes**:

| Capability | Before Istio | After Istio |
|---|---|---|
| Splitting traffic between versions | Not possible without a code change or a second Service | A `weight:` field, live in seconds |
| Encryption between services | None | Automatic mTLS, rotated continuously |
| Fine-grained authorization | Only IP/port (`NetworkPolicy`) | Cryptographic identity + HTTP path/method |
| Retries and timeouts | Would need to be written into the Go app | Centrally configured, enforced by the sidecar |
| Per-service metrics, tracing, live call graph | Would need a metrics/tracing library per service | Automatic, for every service, from day one |

This is the trade that the whole course is about. You gain real abilities.
You pay for them with one extra container in every Pod, and one more system
your team must understand.

Is that trade worth it for your project? [Lesson 1](01-what-is-a-service-mesh.md)
gave you the questions to ask. Now you have actually run the machine
yourself, so you can answer them from your own experience.
