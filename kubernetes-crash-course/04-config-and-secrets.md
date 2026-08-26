# 4. ConfigMaps, Secrets, and Namespaces

## The big idea, in simple words

Your application needs settings: a database address, a port number, a
password. Those settings should **not** be baked into the image.

Why not? Because then you would need to build a different image for
development, for testing, and for production. Same code, three images. That
is wasteful and easy to get wrong.

So Kubernetes keeps settings **outside** the image:

- **ConfigMap** = the **public notice board** in the lobby. Anyone can read
  it. Put non-secret settings here.
- **Secret** = a box marked "private". **Warning:** by default this box has a
  glass door. We will explain this carefully, because it surprises people.
- **Namespace** = a **separate floor** of the building. Two teams can each
  have a room called "api" without any conflict.

One image, many environments. You change the notice board, not the building.

## ConfigMap

A ConfigMap holds plain settings as key–value pairs.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
data:
  PORT: "8080"
  REDIS_ADDR: "redis:6379"
  LOG_LEVEL: "info"
```

Note that values must be **strings**. `PORT: 8080` without quotes is a
number and will be rejected. This catches everyone once.

### Using it: as environment variables

The simplest way, and what our project uses:

```yaml
containers:
  - name: api
    image: docker-crash-course-api:dev
    envFrom:
      - configMapRef:
          name: api-config      # every key becomes an env var
```

Or pick individual keys, which is more explicit and easier to read:

```yaml
    env:
      - name: REDIS_ADDR
        valueFrom:
          configMapKeyRef:
            name: api-config
            key: REDIS_ADDR
```

This connects directly to the Go code from the Docker course.
[`main.go`](../docker-crash-course/app/main.go) reads
`os.Getenv("REDIS_ADDR")`. It does not know or care whether that value came
from Docker Compose, a ConfigMap, or your shell. **The application code
never changes.**

### Using it: as files

A ConfigMap can also appear as files inside the container:

```yaml
    volumeMounts:
      - name: config
        mountPath: /etc/config
        readOnly: true
volumes:
  - name: config
    configMap:
      name: api-config
```

Now `/etc/config/PORT` is a file containing `8080`.

Use files when your application expects a config file (`nginx.conf`,
`application.yaml`) instead of environment variables.

| Method | Good for | Limitation |
|---|---|---|
| Environment variables | Simple values | **Frozen at pod start** — never updates |
| Mounted files | Config files, large values | Updates automatically after about a minute |

### Changing a ConfigMap does not restart your pods

This surprises everyone, so remember it:

> Editing a ConfigMap does **not** restart the Pods using it.

If you used environment variables, the running Pods keep the **old** values
forever. Environment variables are read once, when the process starts.

To apply the change, restart the Pods yourself:

```bash
kubectl rollout restart deploy/api
```

Helm users solve this automatically by putting a checksum of the ConfigMap
into the Pod template annotations. When the ConfigMap changes, the checksum
changes, so the Pod template changes, so a rollout happens by itself. See
[lesson 13](13-helm-intro.md).

## Secret — and the truth about it

A Secret looks almost identical to a ConfigMap:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: api-secrets
type: Opaque
stringData:
  DATABASE_URL: "postgres://postgres:postgres@postgres:5432/appdb?sslmode=disable"
  POSTGRES_PASSWORD: "postgres"
```

Use `stringData` when writing YAML by hand — you type normal text and
Kubernetes encodes it for you. The `data` field expects you to encode it
yourself.

You consume it the same way as a ConfigMap:

```yaml
    envFrom:
      - secretRef:
          name: api-secrets
```

### ⚠️ Secrets are NOT encrypted by default

This is the single most important security fact in this lesson.

A Kubernetes Secret is stored as **base64**. Base64 is **not encryption**.

Base64 is like writing a message in a **different alphabet**. It is a
costume, not a lock. Anyone can undo it instantly, with no key and no
password:

```bash
kubectl get secret api-secrets -o jsonpath='{.data.DATABASE_URL}' | base64 -d
# postgres://postgres:postgres@postgres:5432/appdb?sslmode=disable
```

That is the whole "protection". One short command.

So what is a Secret actually good for? Three real things:

1. Kubernetes **does not print** its contents in normal command output or
   logs by accident.
2. You can control **who may read it** with RBAC (see
   [lesson 5](05-rbac-and-networkpolicy.md)). This is the genuinely valuable
   part.
3. It can be stored **encrypted at rest** in etcd — but **only if the cluster
   administrator turned that on**. It is off by default in many clusters.

Understand the difference clearly:

| Thing | Protects against |
|---|---|
| base64 | Nothing at all. It is not security. |
| RBAC on Secrets | People and pods reading things they should not |
| Encryption at rest (etcd) | Someone stealing the disk or an etcd backup |
| **A real secrets manager** | Everything above, plus rotation and audit logs |

### Never commit a Secret to git

Because base64 is not encryption, a Secret YAML file in git is the same as a
password in git. Anyone with repository access can read it. And git history
keeps it forever, even if you delete the file later — exactly like the image
layer problem from
[Docker lesson 6](../docker-crash-course/06-image-security.md).

### The real answer: External Secrets Operator

For real workloads, especially anything handling identity or user accounts,
do not store the real secret in Kubernetes at all.

Instead, keep it in a proper secrets manager (AWS Secrets Manager, HashiCorp
Vault, Google Secret Manager) and let a controller **copy it in** when
needed.

**External Secrets Operator (ESO)** does exactly this. You commit a file that
says *where the secret lives*, never the secret itself:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: api-secrets
spec:
  refreshInterval: 1h            # re-read the source every hour
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: api-secrets            # the K8s Secret it will create for you
  data:
    - secretKey: DATABASE_URL    # the key inside the K8s Secret
      remoteRef:
        key: prod/api/database-url   # the name in AWS Secrets Manager
```

Read that file carefully: **there is no password in it.** It is only an
address. This file is safe to commit to git.

ESO then reads AWS Secrets Manager and creates a normal Kubernetes Secret for
you. Your Pod uses it as usual with `envFrom`. And because it refreshes on a
schedule, rotating the password in AWS eventually rotates it in the cluster
too.

```
AWS Secrets Manager  ──►  ESO controller  ──►  Kubernetes Secret  ──►  Pod
  (the real vault)         (copies it)          (short-lived copy)
```

### The other option: Secrets Store CSI driver

The **AWS Secrets Manager CSI driver** takes a different approach: it mounts
the secret **directly into the Pod as a file**, without creating a
Kubernetes Secret object at all.

| | External Secrets Operator | Secrets Store CSI driver |
|---|---|---|
| Creates a K8s Secret? | Yes | No (optional) |
| Secret visible via `kubectl get secret`? | Yes | No |
| App reads it as | Env var or file | File |
| Best when | You want normal K8s Secret behaviour | You want the value to never exist as a K8s object |

The CSI driver is stricter, because the value never becomes a Kubernetes
object that someone could read with RBAC. ESO is easier and works with any
application that expects environment variables.

**Both are correct choices.** Both are far better than committing a Secret
YAML file. For an identity or authentication workload, use one of them.

### Which pods can read your secrets?

One more thing people forget. Access is granted to the Pod's
**ServiceAccount**, not to your user. If any Pod in the namespace can read
the Secret, then anyone who can run a Pod in that namespace can read it too.

This is why namespaces and RBAC matter, and it leads directly into the next
lesson.

## Namespace

A **Namespace** divides one cluster into separate sections. Think of them as
**floors of the building**.

```bash
kubectl create namespace demo
kubectl apply -f manifests/ -n demo
kubectl get pods -n demo

# stop typing -n every time:
kubectl config set-context --current --namespace=demo
```

What namespaces give you:

- **Names do not collide.** A Service called `api` on floor 3 and another
  called `api` on floor 5 are different things.
- **RBAC boundaries.** You can allow a team to work on their floor only.
- **Resource quotas.** You can limit how much CPU and memory one floor may
  use in total.
- **NetworkPolicy targets.** You can allow or block traffic between floors.

What namespaces do **not** give you:

- **They are not a security wall by themselves.** Without a NetworkPolicy,
  Pods on floor 3 can freely talk to Pods on floor 5. Namespaces organise
  names; they do not stop network traffic. This is exactly what
  [lesson 5](05-rbac-and-networkpolicy.md) fixes.

### Cross-namespace DNS

To call a Service on another floor, add the namespace to the name:

```
http://api            # same namespace
http://api.demo       # the `api` service on the `demo` floor
```

### Namespaces you already have

```bash
kubectl get namespaces
```

| Namespace | What it holds |
|---|---|
| `default` | Where things go when you do not choose. **Avoid using it for real work.** |
| `kube-system` | Kubernetes' own components. Do not modify these. |
| `kube-public` | Readable by everyone; rarely used. |

Always create a namespace for your application. `default` becomes a mess very
quickly, and it makes cleanup dangerous.

## What our project uses

| Setting | Where it lives | Why |
|---|---|---|
| `PORT`, `REDIS_ADDR` | ConfigMap | Not secret |
| `DATABASE_URL`, `POSTGRES_PASSWORD` | Secret | Contains a password |
| Everything | Namespace `demo` | Keeps `default` clean, easy to delete later |

In a real deployment, that Secret would be produced by ESO from AWS Secrets
Manager instead of being written by hand.

Next: [RBAC, ServiceAccounts, and NetworkPolicy](05-rbac-and-networkpolicy.md).
