# 4. Security: mTLS and AuthorizationPolicy

## The big idea, in simple words

Two different security questions, answered by two different objects — the
same split you already learned in
[Kubernetes lesson 5](../kubernetes-crash-course/05-rbac-and-networkpolicy.md),
one layer up:

| Question | Object | In the building story |
|---|---|---|
| **Is this call encrypted, and do I know who is really calling?** | `PeerAuthentication` | Every assistant checks a photo ID before letting a call through, and speaks in a private code nobody else can decode |
| **Even if I know who you are, are you allowed to ask for *this*?** | `AuthorizationPolicy` | The rulebook: "the checkout assistant may call the payments room; nobody else may" |

`PeerAuthentication` answers **identity and encryption**.
`AuthorizationPolicy` answers **permission**. You need both, for the same
reason a locked building still has rules about which floors your keycard
opens.

## mTLS: mutual TLS, explained simply

Plain **TLS** — what your browser uses for `https://` — is one-directional:
the website proves who it is to you, but you don't prove who you are to it.

**Mutual TLS (mTLS)** means **both sides prove their identity**. The API
proves it is really the API to Postgres's sidecar, and Postgres's sidecar
proves it is really Postgres back. Neither side has to trust the network in
between — a rogue Pod cannot pretend to be one or the other.

```
   api's sidecar                          postgres's sidecar
  ┌───────────────┐                      ┌───────────────┐
  │ "I am api,     │ ── encrypted ──────▶│ "I am postgres,│
  │  here's proof" │      channel        │  here's proof" │
  └───────────────┘ ◀── both checked ────└───────────────┘
```

Here is the part that makes Istio genuinely powerful: **your application
never sees any of this.** The Go code in
[`main.go`](../docker-crash-course/app/main.go) still opens a plain,
unencrypted connection to `postgres:5432`. The **sidecars** on both ends
silently upgrade that connection to mutual TLS before it ever leaves the
Pod, and silently unwrap it again on arrival. Zero code changes — the exact
same promise from [lesson 1](01-what-is-a-service-mesh.md).

### Where do the certificates come from?

`istiod` runs its own tiny certificate authority. Every sidecar
automatically requests a short-lived certificate from it, and **istiod
rotates them continuously** — by default every 24 hours, invisibly. You
never manage a certificate by hand. Compare this to
[Docker lesson 6](../docker-crash-course/06-image-security.md), where a
missing CA bundle in a `scratch` image would break outbound HTTPS entirely
— here, `istiod` *is* the CA, for every service in the mesh.

## PeerAuthentication: turning mTLS on

```yaml
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: demo
spec:
  mtls:
    mode: STRICT
```

`mode: STRICT` means: **only mTLS connections are accepted.** A plaintext
connection from outside the mesh (or from a Pod without a sidecar) is
rejected outright.

| Mode | Behaviour |
|---|---|
| `STRICT` | Only mTLS accepted. Safest. |
| `PERMISSIVE` | Accepts both mTLS and plaintext, on the same port |
| `DISABLE` | mTLS turned off entirely for the selected workloads |

`PERMISSIVE` is the default when you first install Istio, and it exists for
one practical reason: **migration**. If you turn on `STRICT` everywhere
before every single Pod has a sidecar, the Pods without one are instantly
cut off from the mesh. You would cause your own outage. `PERMISSIVE` lets mTLS
and plaintext coexist while you inject sidecars namespace by namespace, and
you flip to `STRICT` only once you have confirmed every relevant Pod has
one.

Apply it narrowly to one namespace (as above) or to the whole mesh:

```yaml
metadata:
  name: default
  namespace: istio-system   # mesh-wide, in the control plane's namespace
```

### Proving it actually happened

```bash
istioctl authn tls-check api-<pod-name>.demo postgres.demo.svc.cluster.local
```

This prints whether the connection from `api` to `postgres` is using mTLS,
and which `PeerAuthentication` rule caused it. It is the mesh equivalent of
[Kubernetes lesson 3](../kubernetes-crash-course/03-services-and-ingress.md)'s
`kubectl get endpoints` — the one command to check before assuming anything
is wrong.

## AuthorizationPolicy: who may call whom

`STRICT` mTLS only proves identity — by default, once mTLS is on, **any
service in the mesh may still call any other service**, exactly as with the
"flat network" default described in
[Kubernetes lesson 5](../kubernetes-crash-course/05-rbac-and-networkpolicy.md).
Proving who you are is not the same as being allowed in.

```yaml
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: postgres-allow-api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: postgres              # this policy protects POSTGRES
  action: ALLOW
  rules:
    - from:
        - source:
            principals: ["cluster.local/ns/demo/sa/api"]
      to:
        - operation:
            ports: ["5432"]
```

Read this exactly like the `NetworkPolicy` from
[Kubernetes lesson 5](../kubernetes-crash-course/05-rbac-and-networkpolicy.md):
attached to the **receiving** side (`postgres`), describing who may come
**in**. But look at what identifies the caller: `principals:
["cluster.local/ns/demo/sa/api"]` — that is the **ServiceAccount** the
caller's Pod runs as, cryptographically proven by mTLS. It is not an IP
address, and it cannot be spoofed by a Pod claiming a different label.

This is meaningfully stronger than `NetworkPolicy`, which only checks Pod
labels — anyone who can create a Pod with the right label passes a
`NetworkPolicy` check. Passing an `AuthorizationPolicy` check requires
possessing the actual cryptographic identity of that ServiceAccount, which
only Pods **legitimately running as it** ever have.

### Default-deny, the same pattern as before

Exactly like
[Kubernetes lesson 5](../kubernetes-crash-course/05-rbac-and-networkpolicy.md)'s
`default-deny-all`, an empty selector with no rules blocks everything:

```yaml
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: default-deny-all
  namespace: demo
spec:
  {}
```

An empty `spec: {}` applies to every workload in the namespace and allows
nothing. Add specific `ALLOW` policies afterward, one relationship at a
time — the identical "start at zero, open exactly what's needed" discipline
from the Kubernetes course.

### Authorization can inspect HTTP, not just ports

Because `AuthorizationPolicy` operates at the application layer, it can do
things `NetworkPolicy` fundamentally cannot — allow specific paths or
methods, not just a whole port:

```yaml
spec:
  selector:
    matchLabels:
      app: api
  action: ALLOW
  rules:
    - from:
        - source:
            principals: ["cluster.local/ns/demo/sa/api-gateway-caller"]
      to:
        - operation:
            methods: ["GET"]
            paths: ["/items", "/healthz", "/readyz"]
```

A caller with this identity may `GET /items`, but not `POST /items` and not
`DELETE` anything — a distinction plain Kubernetes networking has no
vocabulary for at all.

## How Istio security and Kubernetes security stack together

You now have **three independent security layers**, each answering a
different question, each enforced by a different component:

```
                 "May A even open a connection to B?"
                          NetworkPolicy (Calico)          — L3/L4, IP+port

                          ▼

                 "Is this connection encrypted, and is
                  the caller's identity cryptographically proven?"
                          PeerAuthentication (Istio)       — mTLS

                          ▼

                 "Given a proven identity, is THIS
                  specific request allowed?"
                          AuthorizationPolicy (Istio)      — L7, path+method

                          ▼

                 "Does the calling POD have permission to
                  talk to the Kubernetes API itself?"
                          RBAC + ServiceAccount (K8s)      — cluster API access
```

Note that the first three protect **service-to-service traffic**. RBAC
(from
[Kubernetes lesson 5](../kubernetes-crash-course/05-rbac-and-networkpolicy.md))
protects something different entirely — whether a Pod's ServiceAccount may
call the **Kubernetes API server** itself. A Pod can have zero Kubernetes
RBAC permissions (as our `api` ServiceAccount deliberately does) and still
need an `AuthorizationPolicy` to control what it may say to `postgres`.
Locking one says nothing about the other — the same warning from
[Kubernetes lesson 5](../kubernetes-crash-course/05-rbac-and-networkpolicy.md)
applies here with one more layer added.

## Try it

Apply strict mTLS and a default-deny authorization policy in `demo`:

```bash
kubectl apply -f manifests/peerauthentication-strict.yaml
kubectl apply -f manifests/authorizationpolicy-default-deny.yaml
```

The API should now be **completely unreachable**, even through the Gateway
— exactly like applying `default-deny-all` in the Kubernetes course broke
the app on purpose. Confirm:

```bash
curl http://api.local/healthz     # should fail/timeout
```

Now add the specific allow rules and confirm it works again:

```bash
kubectl apply -f manifests/authorizationpolicy-allow.yaml
curl http://api.local/healthz     # works again
```

Finally, prove the identity check is real, not just a label check. Launch a
plain Pod with **no ServiceAccount matching any allow rule** and try to
reach Postgres directly — it should be refused, the same test you ran with
`NetworkPolicy` in
[Kubernetes lesson 6](../kubernetes-crash-course/06-deploy-to-kind.md), now
enforced one layer deeper:

```bash
kubectl run attacker --rm -it --restart=Never -n demo --image=busybox -- \
  sh -c 'wget -qO- --timeout=3 http://postgres:5432 || echo BLOCKED'
```

Next: [Resilience: retries, timeouts, circuit breaking](05-resilience.md).
