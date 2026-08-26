# 2. Installing Istio on Your Local Cluster

## What you need

| Tool | Install (macOS) | What it is |
|---|---|---|
| `istioctl` | `brew install istioctl` | Istio's own CLI — installs and inspects the mesh |
| `kubectl` | already installed | From the Kubernetes course |
| A running kind cluster | from the [Kubernetes course](../kubernetes-crash-course/06-deploy-to-kind.md) | The cluster we install Istio into |

If you deleted your cluster, recreate it first — this course assumes the
`demo` namespace with the API, Postgres, and Redis from the Kubernetes
course is already running:

```bash
cd ../kubernetes-crash-course
kind create cluster --name k8s-course --config manifests/kind-config.yaml
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/calico.yaml
kubectl wait --for=condition=Ready pods --all -n kube-system --timeout=300s
kind load docker-image docker-crash-course-api:dev --name k8s-course
kubectl apply -f manifests/base/
kubectl get pods -n demo
```

## Choosing an installation profile

Istio ships several **profiles** — pre-set bundles of components. On a
laptop, resource usage matters, so picking the right one matters.

| Profile | What it installs | Use it for |
|---|---|---|
| `minimal` | Only `istiod` (the control plane) | Not useful alone — nothing routes traffic in from outside |
| `default` | `istiod` + an ingress gateway | **What we use** — a realistic, moderate footprint |
| `demo` | Everything, including sample telemetry add-ons at higher resource requests | Istio's own docs and demos — heavier than we need |

We use `default`, with resource requests **lowered** for a laptop. Without
lowering them, `istiod` and the gateway alone can ask for more CPU than a
3-node kind cluster comfortably has spare, and pods sit `Pending` — the
exact symptom from
[Kubernetes lesson 8](../kubernetes-crash-course/08-resources-and-qos.md).

[`manifests/istio-install.yaml`](manifests/istio-install.yaml) is an
`IstioOperator` resource that does this:

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: laptop-install
spec:
  profile: default
  components:
    pilot:                        # istiod itself
      k8s:
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
    ingressGateways:
      - name: istio-ingressgateway
        enabled: true
        k8s:
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
          service:
            type: NodePort         # kind has no cloud LoadBalancer — see
                                    # Kubernetes lesson 3 on Service types
  meshConfig:
    accessLogFile: /dev/stdout     # so `kubectl logs` on the sidecar shows traffic
```

Read `pilot` as "the control plane's own resource budget," and
`ingressGateways` as "the resource budget for the front-door proxy" — the
mesh's equivalent of the nginx Ingress controller from
[Kubernetes lesson 6](../kubernetes-crash-course/06-deploy-to-kind.md).

## Install it

```bash
cd ../istio-crash-course
istioctl install -f manifests/istio-install.yaml -y
```

> **One note about this file format.** The file uses a type called
> `IstioOperator`. `istioctl install -f` still accepts it as a settings
> file, which is how we use it.
>
> But do **not** run `kubectl apply -f` on it. There used to be a
> controller you could install in the cluster to watch these objects. That
> controller is now removed. For a real production cluster, use the
> official Helm charts instead — see
> [Kubernetes lesson 13](../kubernetes-crash-course/13-helm-intro.md) for
> what Helm is.

This takes a minute or two. Watch it come up:

```bash
kubectl get pods -n istio-system
```

```
NAME                                    READY   STATUS    RESTARTS
istiod-7d4b8c9f5-2xk9p                  1/1     Running   0
istio-ingressgateway-6f8d9c7b4-lm3xz    1/1     Running   0
```

Verify the install is healthy:

```bash
istioctl verify-install
```

## Turning on sidecar injection for our namespace

Installing Istio does **not** add sidecars to any existing Pods. Injection
is **opt-in**, per namespace, using a label:

```bash
kubectl label namespace demo istio-injection=enabled
```

This label tells `istiod`'s admission webhook: "any new Pod created in this
namespace should get a sidecar automatically." It only affects **new**
Pods — existing ones are untouched until they restart.

```bash
kubectl rollout restart deployment -n demo
kubectl rollout restart statefulset -n demo
```

Now check the Pods again:

```bash
kubectl get pods -n demo
```

```
NAME                     READY   STATUS    RESTARTS
api-7d4b8c9f5-2xk9p      2/2     Running   0
postgres-0               2/2     Running   0
redis-6f8d9c7b4-lm3xz    2/2     Running   0
```

**`2/2`, not `1/1`.** That second container in every Pod is the injected
`istio-proxy` sidecar. Confirm it directly:

```bash
kubectl get pod -n demo -l app=api -o jsonpath='{.items[0].spec.containers[*].name}'
# api istio-proxy
```

## ⚠️ A real collision with our production security settings

This is worth understanding, not skipping past.

In [Kubernetes lesson 14](../kubernetes-crash-course/14-production-readiness.md)
we hardened the API Deployment with:

```yaml
securityContext:
  readOnlyRootFilesystem: true
  capabilities:
    drop: ["ALL"]
```

Sidecar injection normally adds an **init container** (`istio-init`) that
needs `NET_ADMIN` and `NET_RAW` — real, privileged network capabilities — to
set up the `iptables` rules that redirect your Pod's traffic through the
sidecar. That is a much bigger permission than anything our own app
container needs, and in a cluster enforcing the restricted **Pod Security
Standard**, it can be **rejected outright**.

Two ways to resolve this, from more to less invasive:

1. **Install the Istio CNI plugin.** It moves the `iptables` setup out of a
   per-Pod init container and into a single privileged **node-level**
   DaemonSet, installed once. Individual application Pods no longer need any
   elevated capability at all — the CNI plugin does the redirect for them.
   This is the recommended approach for any cluster enforcing strict Pod
   Security.
   ```bash
   istioctl install -f manifests/istio-install.yaml --set components.cni.enabled=true -y
   ```
2. **Loosen the namespace's Pod Security level**, only if you cannot install
   the CNI plugin. This trades away real protection, so treat it as a
   fallback, not a default.

For this local course, either works — kind does not enforce restricted Pod
Security by default. In a real cluster, prefer option 1.

## Verifying traffic actually flows through the mesh

The proof is in the sidecar's own logs. Because we set
`accessLogFile: /dev/stdout` above, every request the sidecar handles is
printed:

```bash
curl http://api.local/healthz     # generate one request (see lesson 3 for the Gateway)
kubectl logs -n demo -l app=api -c istio-proxy --tail=5
```

You should see a structured access log line for the request — proof the
sidecar intercepted it, not just the application's own log line.

## What you have now

```
                    istio-system namespace
             ┌────────────────────────────────┐
             │   istiod          ingressgateway │
             └────────────────────────────────┘
                              │
                    configures every sidecar
                              │
                    demo namespace
             ┌────────────────────────────────┐
             │  api (2/2)   postgres-0 (2/2)   │
             │              redis (2/2)         │
             └────────────────────────────────┘
```

Every Pod in `demo` now has an assistant at the door. Right now those
assistants are just forwarding calls with no special rules — the next
lesson teaches them to route, split, and shape traffic.

## Clean up (if you want to remove Istio but keep the cluster)

```bash
kubectl label namespace demo istio-injection-
kubectl rollout restart deployment -n demo
kubectl rollout restart statefulset -n demo
istioctl uninstall --purge -y
kubectl delete namespace istio-system
```

Next: [Traffic management](03-traffic-management.md).
