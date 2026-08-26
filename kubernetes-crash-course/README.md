# Kubernetes Crash Course

The follow-on to the [Docker Crash Course](../docker-crash-course/). There you
learned that a container is like **renting one room** in an apartment
building.

**Kubernetes is the building manager.** It keeps refilling empty apartments,
notices when a tenant is sick, and makes sure the elevator always finds the
right floor.

Every lesson uses simple language and one everyday comparison. The goal is
not only to know *what* to type, but to understand *why*.

You will run the **exact same Go API image** from the Docker course on a real
cluster — no code changes, no Dockerfile changes.

## Part 1 — Fundamentals

| # | Lesson | The idea in one line |
|---|---|---|
| 1 | [Why Kubernetes Exists](01-why-kubernetes.md) | A thermostat for your apps: describe the goal, not the steps |
| 2 | [Pods, ReplicaSets, Deployments](02-pods-replicasets-deployments.md) | Pods are paper cups — replaced, never repaired |
| 3 | [Services and Ingress](03-services-and-ingress.md) | A reception desk inside, a front door outside |
| 4 | [ConfigMaps, Secrets, Namespaces](04-config-and-secrets.md) | A notice board, a weak safe, and separate floors |
| 5 | [RBAC and NetworkPolicy](05-rbac-and-networkpolicy.md) | Who holds which keys, and which rooms may phone which |
| 6 | [**Lab:** Deploy to kind](06-deploy-to-kind.md) | Run the Docker course app on real Kubernetes |

## Part 2 — Production

| # | Lesson | The idea in one line |
|---|---|---|
| 7 | [Health Probes](07-probes.md) | Telling "unconscious" apart from "busy right now" |
| 8 | [Resources and QoS](08-resources-and-qos.md) | CPU gets throttled; memory gets you killed |
| 9 | [Autoscaling](09-autoscaling.md) | More apartments (HPA) vs more buildings (Karpenter) |
| 10 | [Rollouts and Disruption](10-rollouts-and-disruption.md) | Renovate one room at a time, never empty the floor |
| 11 | [Graceful Shutdown](11-graceful-shutdown.md) | **The #1 cause of dropped requests during deploys** |
| 12 | [StatefulSets](12-statefulsets.md) | Temporary staff vs tenants with assigned apartments |
| 13 | [Helm](13-helm-intro.md) | A form letter for Kubernetes |
| 14 | [**Lab:** Production Readiness + Quiz](14-production-readiness.md) | Checklist and 10 questions |

## What you need

```bash
brew install kind kubectl helm     # macOS
```

Plus Docker or Podman, which you already have from the Docker course.

## Quick start

```bash
# 1. build the app image (from the Docker course)
docker build -t docker-crash-course-api:dev ../docker-crash-course/app

# 2. create the cluster
kind create cluster --name k8s-course --config manifests/kind-config.yaml

# 3. install the network plugin (nodes stay NotReady until this finishes)
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/calico.yaml
kubectl wait --for=condition=Ready pods --all -n kube-system --timeout=300s

# 4. install the ingress controller
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.11.2/deploy/static/provider/kind/deploy.yaml
kubectl wait -n ingress-nginx --for=condition=Ready pod \
  --selector=app.kubernetes.io/component=controller --timeout=180s

# 5. load the image INTO the cluster (skipping this = ImagePullBackOff)
kind load docker-image docker-crash-course-api:dev --name k8s-course

# 6. deploy
kubectl apply -f manifests/base/
kubectl get pods -n demo -w

# 7. reach it
echo "127.0.0.1 api.local" | sudo tee -a /etc/hosts
curl http://api.local/healthz
```

Full explanation of every step: [lesson 6](06-deploy-to-kind.md).

Then upgrade to the production version:

```bash
kubectl apply -f manifests/production/
```

## The manifests

```
manifests/
├── kind-config.yaml              # 3 nodes, ingress ports, no default CNI
├── base/                         # Part 1 — the Day-1 deployment
│   ├── 00-namespace.yaml
│   ├── 01-config-and-secret.yaml
│   ├── 02-postgres.yaml          # StatefulSet + headless Service
│   ├── 03-redis.yaml             # Deployment + Service
│   ├── 04-api-serviceaccount.yaml
│   ├── 05-api.yaml               # Deployment + Service + Ingress
│   └── 06-networkpolicy.yaml     # default-deny + explicit allows
└── production/                   # Part 2 — the upgraded version
    ├── api-deployment.yaml       # + probes, resources, preStop, spreading
    └── hpa-and-pdb.yaml          # + autoscaling, disruption budget
```

Every file is commented with the lesson it comes from. Reading
`base/05-api.yaml` and `production/api-deployment.yaml` side by side is the
fastest summary of Part 2.

## Two deliberate choices

**We disable kind's default network plugin and install Calico.** kind's
built-in plugin accepts NetworkPolicy objects and silently enforces
**nothing**. Teaching network security on it would be theatre — you would
believe you were protected while being completely open.

**Postgres runs as a StatefulSet, but you should not do this in production.**
Use a managed database (RDS, Cloud SQL) or a real operator. A StatefulSet
gives you stable names and disks — not backups, failover, or point-in-time
recovery. See [lesson 12](12-statefulsets.md).

## How this connects to the Docker course

| Docker course | Kubernetes course |
|---|---|
| Container = renting a room | Kubernetes = the building manager |
| Compose service DNS | Cluster DNS + Service |
| `depends_on: service_healthy` | No equivalent — crash and retry instead ([6](06-deploy-to-kind.md)) |
| `HEALTHCHECK` (impossible on distroless) | `httpGet` probes, run by the kubelet ([7](07-probes.md)) |
| `/healthz` vs `/readyz` split | Exactly what liveness and readiness consume ([7](07-probes.md)) |
| Named volume for Postgres | PersistentVolumeClaim ([12](12-statefulsets.md)) |
| Non-root, distroless image | `securityContext` enforces it cluster-side ([5](05-rbac-and-networkpolicy.md)) |
| `TARGETARCH` multi-arch builds | Required if Karpenter picks ARM nodes ([9](09-autoscaling.md)) |
| Never use `:latest` | Same rule, now it decides what a rollback means ([10](10-rollouts-and-disruption.md)) |

## Clean up

```bash
kubectl delete namespace demo          # remove the app
kind delete cluster --name k8s-course  # remove everything
```
