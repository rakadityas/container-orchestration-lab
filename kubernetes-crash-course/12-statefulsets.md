# 12. StatefulSets vs Deployments

## The big idea, in simple words

Think about the difference between two kinds of worker.

**Deployment pods are like temporary staff.** They all wear the same uniform
and do the same job. If one goes home, you send any other one instead. Nobody
asks for a specific person. They own nothing.

**StatefulSet pods are like tenants with assigned apartments.** Each has a
number on the door (`postgres-0`, `postgres-1`), a personal mailbox at a
fixed address, and their own storage locker. If they leave and come back,
they get **the same door, the same mailbox, and the same locker**.

| | Deployment | StatefulSet |
|---|---|---|
| Pod names | Random: `api-7d4b8c9f5-2xk9p` | Numbered: `postgres-0`, `postgres-1` |
| Identity after restart | New name, new IP | **Same name**, same DNS address |
| Storage | Shared or none | **One disk per pod**, kept forever |
| Start order | All at once | **One at a time**, 0 then 1 then 2 |
| Stop order | All at once | **Reverse**, highest number first |
| Replacing a pod | Any pod is fine | Must be *that* pod |

## When do I need a StatefulSet?

Ask one question:

> **If I delete this pod and a new one appears, does it need the previous
> pod's data and name?**

- **No** → Deployment. Web servers, APIs, workers, most things.
- **Yes** → StatefulSet. Databases, message brokers, anything clustered.

Real examples:

| Workload | Choice | Why |
|---|---|---|
| Our Go API | Deployment | Any copy serves any request; owns no data |
| Postgres | **StatefulSet** | Owns its data files; replicas must know who is primary |
| Redis as a cache | Deployment | Data is disposable |
| Redis as a datastore | **StatefulSet** | Data must survive |
| Kafka, Elasticsearch, MongoDB | **StatefulSet** | Cluster members must identify each other |

> **Important:** a StatefulSet does **not** make your application clustered.
> It only provides stable names and disks. Running 3 Postgres replicas in a
> StatefulSet does **not** give you replication — Postgres itself must be
> configured for that. The StatefulSet only makes such a setup possible.

## Stable network identity

A StatefulSet needs a **headless Service** (`clusterIP: None`, from
[lesson 3](03-services-and-ingress.md)). This gives every pod its own DNS
name:

```
<pod-name>.<service-name>.<namespace>.svc.cluster.local

postgres-0.postgres.demo.svc.cluster.local
postgres-1.postgres.demo.svc.cluster.local
```

These names **never change**. If `postgres-0` dies, the replacement is also
called `postgres-0` and answers at the same address.

This is exactly what a database cluster needs. A replica must be able to say
"my primary is `postgres-0`" and have that stay true after any restart. With
a Deployment's random names, that sentence could not even be written.

## Stable storage: `volumeClaimTemplates`

This is the StatefulSet's most useful feature.

```yaml
  volumeClaimTemplates:
    - metadata:
        name: pgdata
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 1Gi
```

For each pod, Kubernetes creates its **own** PersistentVolumeClaim:

```
postgres-0  →  pgdata-postgres-0   (its own 1Gi disk)
postgres-1  →  pgdata-postgres-1   (a different 1Gi disk)
```

Two guarantees follow:

1. If `postgres-0` is deleted and recreated, it **reattaches to
   `pgdata-postgres-0`**. Its data is still there.
2. Deleting the StatefulSet **does not delete the disks**. This is deliberate
   protection against accidentally destroying a database.

```bash
kubectl get pvc -n demo
kubectl delete statefulset postgres -n demo     # pods go, DISKS REMAIN
kubectl get pvc -n demo                          # still there
kubectl delete pvc pgdata-postgres-0 -n demo    # only this actually erases data
```

Remember this when cleaning up: `kubectl delete -f manifests/` leaves the
PVCs behind. Deleting the whole namespace removes them.

## Ordered start and stop

```
Scaling UP        Scaling DOWN
postgres-0        postgres-2   ← removed first (highest number)
postgres-1        postgres-1
postgres-2        postgres-0   ← removed last
```

Each pod must be **Ready** before the next one starts. This matters for
databases where the first pod initialises the cluster and later ones join it.

The order is also why StatefulSet rollouts are slower: pods are updated one
at a time, in reverse order.

If you do not need ordering (and it makes startup slow), you can turn it off:

```yaml
spec:
  podManagementPolicy: Parallel     # default is OrderedReady
```

## Our Postgres

Look at [`manifests/base/02-postgres.yaml`](manifests/base/02-postgres.yaml):

```yaml
spec:
  serviceName: postgres        # must match the headless Service
  replicas: 1
  ...
  volumeClaimTemplates:
    - metadata:
        name: pgdata
```

Prove the stable identity works:

```bash
# write some data
curl -X POST http://api.local/items -d '{"name":"survives restart"}'

# destroy the database pod completely
kubectl delete pod postgres-0 -n demo

# wait for it to come back, then read the data again
kubectl wait --for=condition=Ready pod/postgres-0 -n demo --timeout=120s
curl http://api.local/items
```

Your item is still there. The new `postgres-0` reattached to the same disk.

Do the same with an API pod and nothing is lost either — because the API pod
stored nothing in the first place. That difference is exactly why one is a
StatefulSet and the other is a Deployment.

## The honest advice about databases

You *can* run Postgres in Kubernetes. This course does it, and it works.

But for production, seriously consider a managed database (AWS RDS, Cloud
SQL) instead. Backups, failover, patching, and point-in-time recovery are
genuinely difficult, and a StatefulSet gives you none of them — it only gives
stable names and disks.

If you do run databases in Kubernetes, use a purpose-built **operator**
(CloudNativePG, Zalando Postgres Operator, Crunchy). An operator is a
controller that understands the database: it handles failover, backups, and
version upgrades. A plain StatefulSet does not.

| Approach | Effort | Use when |
|---|---|---|
| Managed service (RDS) | Lowest | Almost always, in production |
| Operator (CloudNativePG) | Medium | You need it in-cluster for a real reason |
| Plain StatefulSet | Highest risk | Learning, development, testing |

Ours is a plain StatefulSet because this is a learning cluster.

Next: [Helm, an introduction](13-helm-intro.md).
