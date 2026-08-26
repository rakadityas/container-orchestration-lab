# 6. Lab: Deploy to a Local Cluster

This is the hands-on lesson for Part 1. You will run the **exact same image**
from the Docker course on a real Kubernetes cluster on your laptop.

Nothing about the application changes. No code edits, no Dockerfile edits.
Only the surrounding instructions change.

## What you need

| Tool | Install (macOS) | What it is |
|---|---|---|
| `kind` | `brew install kind` | Runs Kubernetes inside Docker containers |
| `kubectl` | `brew install kubectl` | Your telephone to the cluster |
| Docker or Podman | already installed | Needed by kind |

### kind or minikube?

Both create a cluster on your laptop. Either is fine.

| | kind | minikube |
|---|---|---|
| Runs Kubernetes as | Docker containers | A virtual machine or containers |
| Speed | Faster to create and delete | A little slower |
| Multi-node | Easy | Possible |
| Load a local image | `kind load docker-image` | `minikube image load` |

This course uses **kind**, because loading a locally built image is simple
and multi-node clusters are easy — and we need two workers later for
[lesson 10](10-rollouts-and-disruption.md).

## Step 1: build the image

We reuse the image from the Docker course.

```bash
cd ../docker-crash-course
docker build -t docker-crash-course-api:dev ./app
```

If you use Podman, add one step, because kind reads images from Docker's
storage:

```bash
podman build -t docker-crash-course-api:dev ./app
podman save docker-crash-course-api:dev -o /tmp/api.tar
```

## Step 2: create the cluster

```bash
cd ../kubernetes-crash-course
kind create cluster --name k8s-course --config manifests/kind-config.yaml
```

This takes a minute or two. Then check it:

```bash
kubectl get nodes
```

You should see three nodes, and they will all say **`NotReady`**.

**That is expected.** Do not panic. The nodes have no network plugin yet,
because [`manifests/kind-config.yaml`](manifests/kind-config.yaml) sets
`disableDefaultCNI: true`.

Here is why we did that. kind's built-in plugin accepts NetworkPolicy objects
and **silently enforces nothing**. If we used it, [lesson
5](05-rbac-and-networkpolicy.md) would be a lie — you would apply security
rules and be completely unprotected. So we install a plugin that really works.

## Step 3: install Calico (the network plugin)

```bash
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/calico.yaml

# wait for it — this takes 1-3 minutes
kubectl wait --for=condition=Ready pods --all -n kube-system --timeout=300s
kubectl get nodes
```

Now the nodes should say `Ready`. Calico both connects your pods **and**
enforces NetworkPolicy.

## Step 4: install the Ingress controller

Remember from [lesson 3](03-services-and-ingress.md): an Ingress object is
only a description. Something must actually move the traffic.

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.11.2/deploy/static/provider/kind/deploy.yaml

kubectl wait --namespace ingress-nginx \
  --for=condition=Ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=180s
```

Note this is the **kind-specific** version of the file. It is built to use
the `extraPortMappings` from our cluster config, so `localhost:80` on your
laptop reaches the controller.

## Step 5: load the image into the cluster

This step catches everyone the first time.

Your image lives in Docker's storage on your laptop. The kind nodes are
**separate containers** with their own image storage. They cannot see your
laptop's images.

```bash
kind load docker-image docker-crash-course-api:dev --name k8s-course
```

If you used Podman in step 1:

```bash
kind load image-archive /tmp/api.tar --name k8s-course
```

> **If you skip this step**, your pods fail with `ImagePullBackOff`.
> Kubernetes tries to download `docker-crash-course-api:dev` from Docker Hub,
> where it does not exist. This is the most common beginner error in local
> Kubernetes, and now you know exactly what it means.

The manifest sets `imagePullPolicy: IfNotPresent` so Kubernetes uses the
loaded copy instead of trying the internet.

## Step 6: deploy everything

```bash
kubectl apply -f manifests/base/
```

The files are numbered so they apply in a sensible order:

| File | What it creates |
|---|---|
| `00-namespace.yaml` | The `demo` namespace |
| `01-config-and-secret.yaml` | ConfigMap + Secret |
| `02-postgres.yaml` | StatefulSet + headless Service |
| `03-redis.yaml` | Deployment + Service |
| `04-api-serviceaccount.yaml` | ServiceAccount with no permissions |
| `05-api.yaml` | Deployment + Service + Ingress |
| `06-networkpolicy.yaml` | Default-deny, then specific allow rules |

Watch it come up:

```bash
kubectl get pods -n demo -w      # -w = watch live, Ctrl+C to stop
```

You will see the API pods restart a few times at first. This is **normal and
educational**: Postgres takes a while to initialise, and the Go app calls
`log.Fatalf` if it cannot connect at startup, so it exits and Kubernetes
restarts it.

This is the same race condition from
[Docker lesson 5](../docker-crash-course/05-docker-compose-stack.md). Compose
solved it with `depends_on: condition: service_healthy`. **Kubernetes has no
`depends_on` at all.** Its answer is different: let it crash, and keep
restarting until the dependency is ready.

That sounds careless, but it is the more robust design. It also handles
Postgres failing at 3 AM, long after startup — something `depends_on` would
never help with. Design for "retry forever", not for "start in the right
order".

Wait until everything is running:

```bash
kubectl get pods -n demo
```

```
NAME                     READY   STATUS    RESTARTS   AGE
api-7d4b8c9f5-2xk9p      1/1     Running   2          2m
api-7d4b8c9f5-8mn4q      1/1     Running   2          2m
api-7d4b8c9f5-vp7wr      1/1     Running   1          2m
postgres-0               1/1     Running   0          2m
redis-6f8d9c7b4-lm3xz    1/1     Running   0          2m
```

Notice `postgres-0`. StatefulSet pods get **numbered, predictable names**,
unlike the random names of Deployment pods. That is the whole point of a
StatefulSet ([lesson 12](12-statefulsets.md)).

## Step 7: reach the application

Two ways.

### Through the Ingress (the realistic way)

Our Ingress answers for the hostname `api.local`, so tell your laptop that
this name means localhost:

```bash
echo "127.0.0.1 api.local" | sudo tee -a /etc/hosts
```

Then:

```bash
curl http://api.local/healthz
curl http://api.local/readyz
curl -X POST http://api.local/items -d '{"name":"from kubernetes"}'
curl http://api.local/items
curl -X POST http://api.local/items/1/hit
```

Follow the path of that request:

```
your laptop           curl http://api.local/
     │
     ▼  kind extraPortMapping (localhost:80 → node:80)
nginx Ingress controller
     │  reads Host: api.local, matches our Ingress rule
     ▼
Service "api" (ClusterIP)
     │  picks ONE ready pod
     ▼
one of the 3 api pods, port 8080
     │
     ▼
Postgres and Redis, by DNS name
```

### With port-forward (when Ingress misbehaves)

```bash
kubectl port-forward -n demo svc/api 8080:80
curl localhost:8080/healthz
```

`port-forward` bypasses the Ingress completely and connects straight to the
Service. It is the fastest way to find out whether a problem is in your app
or in your Ingress setup.

## Step 8: see Kubernetes do its job

This is the best part. Delete a pod and watch it come back.

```bash
kubectl get pods -n demo
kubectl delete pod -n demo <one-of-the-api-pod-names>
kubectl get pods -n demo
```

A new pod appears within seconds, with a **new name**. You never asked for
it. The ReplicaSet's control loop counted 2, wanted 3, and fixed the
difference ([lesson 1](01-why-kubernetes.md)).

Try scaling:

```bash
kubectl scale deploy/api -n demo --replicas=5
kubectl get pods -n demo
kubectl scale deploy/api -n demo --replicas=3
```

Try a rollout:

```bash
kubectl set image deploy/api -n demo api=docker-crash-course-api:dev
kubectl rollout status deploy/api -n demo
kubectl get rs -n demo              # note the SECOND ReplicaSet at 0 replicas
kubectl rollout undo deploy/api -n demo
```

## Step 9: prove the NetworkPolicy works

Security you have not tested is not security. Let us verify that a random
pod really cannot reach the database.

```bash
kubectl run attacker --rm -it --restart=Never -n demo \
  --image=busybox -- sh
```

Inside that shell:

```sh
nc -zv -w 3 postgres 5432      # should HANG then fail  -> policy works
nc -zv -w 3 api 80             # should also fail       -> not allowed either
exit
```

The `attacker` pod has no `app: api` label, so rule 5 in
[`06-networkpolicy.yaml`](manifests/base/06-networkpolicy.yaml) does not
allow it in.

Now prove the policy is the actual cause, by removing it:

```bash
kubectl delete networkpolicy allow-api-to-postgres -n demo
# run the attacker test again -> still blocked, because default-deny remains

kubectl delete networkpolicy default-deny-all -n demo
# run the attacker test again -> NOW it connects
```

Put them back when you are done:

```bash
kubectl apply -f manifests/base/06-networkpolicy.yaml
```

If the attacker pod could reach Postgres from the very beginning, your CNI is
not enforcing policies. Check that Calico installed correctly.

## When something is broken

Work through these in order. This is the same routine every time.

```bash
kubectl get pods -n demo                    # 1. what state is it in?
kubectl describe pod <name> -n demo         # 2. read EVENTS at the bottom
kubectl logs <name> -n demo                 # 3. what did the app say?
kubectl logs <name> -n demo --previous      # 4. what did the DEAD one say?
kubectl get endpoints api -n demo           # 5. does the Service find pods?
kubectl get events -n demo --sort-by=.lastTimestamp   # 6. everything, recent last
```

Common problems in this lab:

| Symptom | Cause | Fix |
|---|---|---|
| `ImagePullBackOff` | Image not loaded into kind | Redo step 5 |
| `Pending` forever | Not enough CPU/memory on nodes | Lower replicas, or check `kubectl describe node` |
| `CrashLoopBackOff` | App cannot reach Postgres | Check the Secret's `DATABASE_URL`, and that `postgres-0` is Ready |
| Ingress gives 404 | Controller missing, or wrong host | Redo step 4; use `-H "Host: api.local"` |
| Ingress gives 503 | No **ready** pods behind the Service | `kubectl get endpoints api -n demo` |
| DNS fails inside pods | NetworkPolicy blocks DNS | Ensure the `allow-dns` policy exists |
| `nodes NotReady` | No CNI installed | Redo step 3 (Calico) |

## Clean up

```bash
kubectl delete namespace demo         # remove the app, keep the cluster
kind delete cluster --name k8s-course # remove everything
```

Deleting the namespace removes every object inside it. This is why we never
work in `default`.

## What you built

You now have the same three services from the Docker course, but with things
Compose could never do:

| | Docker Compose | Kubernetes |
|---|---|---|
| A container dies | It stays dead (unless restarted manually) | Replaced automatically |
| Scaling | `--scale`, on one machine | `replicas`, across many machines |
| Version updates | Stop everything, start everything | One pod at a time, with instant rollback |
| Finding services | Compose network DNS | Cluster DNS + Service |
| Traffic rules | None between containers | NetworkPolicy |

**Part 1 is complete.** Part 2 makes this deployment safe for production:
health probes, resource limits, autoscaling, safe rollouts, and clean
shutdown.

Next: [Health probes](07-probes.md).
