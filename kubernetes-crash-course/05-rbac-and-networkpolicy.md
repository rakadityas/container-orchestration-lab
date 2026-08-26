# 5. RBAC, ServiceAccounts, and NetworkPolicy

## The big idea, in simple words

Two different questions, two different tools. People mix them up constantly,
so keep them separate in your head:

| Question | Tool | In the building story |
|---|---|---|
| **Who may give orders to Kubernetes?** | RBAC | Which doors does your keycard open? |
| **Which pod may talk to which pod?** | NetworkPolicy | Which rooms may phone which rooms? |

RBAC controls the **manager's office**. NetworkPolicy controls the
**telephone lines between apartments**.

A pod can have zero Kubernetes permissions (RBAC) and still send traffic to
your database (network). Locking one does nothing for the other. You need
both.

## Part 1: RBAC — who may give orders

RBAC means **Role-Based Access Control**. It is built from four objects, and
the pattern is simple:

```
   Role            =  a list of allowed actions   ("open doors 1, 2, 3")
   RoleBinding     =  gives that list to someone  ("Ana gets this keycard")
   ServiceAccount  =  the identity of a POD       (a badge for a program)
   User            =  the identity of a HUMAN     (you, via kubeconfig)
```

Two levels exist, and the naming is very literal:

| Object | Scope |
|---|---|
| `Role` + `RoleBinding` | **One namespace** — one floor of the building |
| `ClusterRole` + `ClusterRoleBinding` | **The whole cluster** — every floor |

**Always prefer `Role` over `ClusterRole`.** A `ClusterRole` is a master key.

### A Role: the list of allowed actions

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: demo
  name: config-reader
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
```

Read it as one sentence: *"in the namespace `demo`, you may get, list, and
watch ConfigMaps."* Nothing else. Not Secrets. Not Pods. Not in other
namespaces.

The common verbs:

| Verb | Meaning |
|---|---|
| `get` | Read one named object |
| `list` | Read all of them |
| `watch` | Subscribe to live changes |
| `create` / `update` / `patch` | Write |
| `delete` | Remove |
| `*` | Everything — **avoid this** |

### A ServiceAccount: the badge a pod carries

Every Pod runs as some ServiceAccount. If you do not choose one, it uses the
namespace's `default` ServiceAccount.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: api
  namespace: demo
automountServiceAccountToken: false    # see below — important
```

Then attach it to the Deployment:

```yaml
spec:
  template:
    spec:
      serviceAccountName: api
```

### A RoleBinding: hand the keycard over

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: api-config-reader
  namespace: demo
subjects:
  - kind: ServiceAccount
    name: api
    namespace: demo
roleRef:
  kind: Role
  name: config-reader
  apiGroup: rbac.authorization.k8s.io
```

Now Pods running as the `api` ServiceAccount may read ConfigMaps in `demo`,
and may do nothing else.

```
ServiceAccount "api"  ──RoleBinding──►  Role "config-reader"
   (worn by the pod)                      (get/list/watch configmaps)
```

### The setting most people miss

By default, Kubernetes mounts a **login token** for the ServiceAccount into
every Pod, at `/var/run/secrets/kubernetes.io/serviceaccount/token`.

Our Go API never calls the Kubernetes API. It talks to Postgres and Redis
only. So that token is pure risk: if an attacker gets into the container,
they find a valid cluster credential sitting on disk, ready to use.

Turn it off:

```yaml
automountServiceAccountToken: false
```

**Most applications never need this token.** Only turn it on for programs
that genuinely talk to the Kubernetes API (controllers, operators, some
monitoring agents).

### Checking permissions

This command answers "am I allowed to do this?" and is very useful:

```bash
# can I do it?
kubectl auth can-i delete pods -n demo

# can that SERVICE ACCOUNT do it?
kubectl auth can-i get secrets \
  --as=system:serviceaccount:demo:api -n demo      # should say "no"
```

Use the `--as=` form to test a Pod's real permissions before an attacker
does.

### RBAC rules of thumb

1. **Start with zero.** Add one permission when something actually fails.
2. **Never grant `cluster-admin`** to an application. Ever.
3. **Avoid `verbs: ["*"]` and `resources: ["*"]`.** Write them out.
4. **Give each application its own ServiceAccount.** Never share `default`,
   because permissions given to `default` apply to every pod in the
   namespace.
5. **Watch out for `secrets` + `list`.** That single permission lets a pod
   read every secret in the namespace. Treat it as a serious grant.

## Part 2: NetworkPolicy — which pod may talk to which

### The default is dangerous

By default, in a fresh Kubernetes cluster:

> **Every pod can send traffic to every other pod, in every namespace.**

Your public web frontend can connect directly to your billing database. A
test pod in another namespace can connect to your production Redis. Nothing
blocks it.

This is a **flat network**. Every apartment can phone every other apartment,
including apartments on other floors.

### East-west vs north-south

Two terms you will hear. Think of a compass on a diagram:

```
                  north  (from the internet)
                    │
                    ▼
     west ────  [ your pods ]  ──── east
     (pod ↔ pod, sideways, inside the cluster)
                    │
                    ▼
                  south  (out to a database, an API)
```

- **North-south** = traffic entering or leaving the cluster. This is what
  your Ingress and firewalls handle.
- **East-west** = traffic **between pods inside** the cluster, sideways.

Most people protect north-south and completely forget east-west. But
east-west is exactly the path an attacker uses **after** breaking into one
pod. If everything can reach everything, one compromised pod reaches your
whole system.

NetworkPolicy is the tool for east-west.

### First: default-deny

The correct starting position is **block everything**, then allow only what
is needed. This is called **default-deny**.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: demo
spec:
  podSelector: {}                  # {} means EVERY pod in this namespace
  policyTypes:
    - Ingress                      # block incoming
    - Egress                       # block outgoing
```

`podSelector: {}` is the important trick. An empty selector matches
**everything**. With no `ingress:` or `egress:` rules listed, nothing is
allowed at all.

After applying this, your application **will break**. That is expected and
correct. Now you open exactly the doors you need, one at a time.

### How policies combine

Two rules to memorise:

1. Policies are **additive**. They only ever allow. There is no "deny" rule.
   The final result is the sum of every policy that matches a pod.
2. A pod is **unrestricted until at least one policy selects it**. The moment
   one policy selects it, everything not explicitly allowed is denied.

So a single default-deny policy flips the whole namespace from "allow all" to
"deny all".

### Then: allow what you need

**Let the Ingress controller reach the API:**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-ingress-to-api
  namespace: demo
spec:
  podSelector:
    matchLabels:
      app: api
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: ingress-nginx
      ports:
        - protocol: TCP
          port: 8080
```

**Let the API reach Postgres — and nobody else:**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-api-to-postgres
  namespace: demo
spec:
  podSelector:
    matchLabels:
      app: postgres          # this policy protects POSTGRES
  policyTypes: [Ingress]
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: api       # only pods labelled app=api may connect
      ports:
        - protocol: TCP
          port: 5432
```

Read the last one carefully, because the direction confuses people. The
policy is attached to **Postgres** (`podSelector: app: postgres`) and
describes who may come **in** (`from: app: api`). You write the rule on the
receiving side.

Now, if an attacker takes over some other pod in the namespace, that pod
still cannot open a connection to Postgres. The blast radius is contained.

### Do not forget DNS

This is the most common NetworkPolicy mistake, and the symptom is confusing:
your app suddenly cannot resolve `postgres`, and you see DNS timeouts
everywhere.

The reason: after a default-deny **egress** policy, pods can no longer reach
the cluster DNS server either. Always allow DNS explicitly:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
  namespace: demo
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
```

### ⚠️ Your cluster may ignore NetworkPolicy completely

This is a genuine trap. A NetworkPolicy is only a *description*. Something
must enforce it — the **CNI plugin**, the networking component of your
cluster.

- **Enforce it:** Calico, Cilium, Weave, AWS VPC CNI (with the policy agent
  enabled)
- **Ignore it silently:** the default `kindnet` in kind, and some simple
  setups

If your CNI does not support policies, Kubernetes **accepts your YAML
without any error** and enforces nothing. You believe you are protected, and
you are not.

This is why [lesson 6](06-deploy-to-kind.md) creates the kind cluster with
its default CNI **disabled** and installs Calico instead. Otherwise the
NetworkPolicy part of this course would be pure theatre.

Always test it for real:

```bash
# from a pod that should NOT have access, try to connect
kubectl run test --rm -it --image=busybox --restart=Never -n demo \
  -- wget -qO- --timeout=3 http://postgres:5432
# a hang or timeout = the policy works
# an immediate connection = it is NOT being enforced
```

## Putting both together

For our API, a good security position is:

| Control | Setting |
|---|---|
| ServiceAccount | Its own, with `automountServiceAccountToken: false` |
| RBAC | No Role at all — the app never calls the Kubernetes API |
| NetworkPolicy | Default-deny, then: Ingress → api, api → postgres, api → redis, all → DNS |
| Container user | `nonroot` (already set in the Dockerfile) |

Notice that the safest RBAC configuration here is **nothing**. The
application does not need to talk to Kubernetes, so it gets no permissions at
all. Always ask "does this really need access?" before writing a Role.

Next: [Deploy to a local cluster](06-deploy-to-kind.md).
