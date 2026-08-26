# 3. Services and Ingress

## The big idea, in simple words

Pods are replaced all the time, and every new Pod gets a **new IP address**.
So you can never write a Pod's address in your configuration. By tomorrow it
will be wrong.

A **Service** solves this. It is the **reception desk** of the building.

The reception desk has a permanent phone number that never changes. Tenants
move in and out constantly, but you always call reception, and reception
connects you to whoever is currently living there.

```
        You call ONE number, forever
                    │
                    ▼
            ┌───────────────┐
            │    Service    │   stable IP + stable DNS name
            │  (reception)  │
            └───────┬───────┘
                    │ sends you to any healthy pod
        ┌───────────┼───────────┐
        ▼           ▼           ▼
    ┌───────┐   ┌───────┐   ┌───────┐
    │ Pod   │   │ Pod   │   │ Pod   │   ← these come and go
    │ .0.4  │   │ .0.7  │   │ .1.2  │      with new IPs each time
    └───────┘   └───────┘   └───────┘
```

An **Ingress** is a different thing: it is the **front door of the building
from the street**, with a receptionist who reads where you want to go and
points you to the right internal desk.

- **Service** = talking *inside* the building
- **Ingress** = letting people in *from outside*

## How a Service finds its Pods

Exactly the same way a ReplicaSet does: **labels and selectors**. There is no
list of IP addresses anywhere.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  selector:
    app: api          # send traffic to any pod wearing this label
  ports:
    - port: 80        # the port the Service listens on
      targetPort: 8080  # the port inside the container
```

Read the two ports carefully, because mixing them up is a very common bug:

| Field | Meaning |
|---|---|
| `port` | What **callers** use: `http://api:80` |
| `targetPort` | What the **container** listens on: 8080 |

They can be the same number. They do not have to be.

Because the Service uses a selector, it automatically includes new Pods and
removes dead ones. You do nothing.

### Only *ready* pods receive traffic

This detail matters a lot, and it connects to [lesson 7](07-probes.md).

A Service does not send traffic to every matching Pod. It sends traffic only
to Pods that are **Ready**. A Pod that is starting up, or failing its
readiness probe, is quietly removed from the reception desk's list.

This is how Kubernetes avoids sending a request to an application that is
still loading.

## Built-in DNS

Kubernetes runs a DNS server inside the cluster. Every Service gets a name
automatically.

```bash
http://api                        # same namespace
http://api.demo                   # the `api` service in namespace `demo`
http://api.demo.svc.cluster.local # the full official name
```

This is exactly the same idea as Docker Compose service names from
[Docker lesson 4](../docker-crash-course/04-networking-basics.md). There,
`postgres:5432` worked because Compose created a network with a phone book.
Here, `postgres:5432` works because Kubernetes has cluster DNS.

**Your application code does not change at all.** The connection string
`postgres://postgres:postgres@postgres:5432/appdb` from the Docker course
works in Kubernetes without editing a single line, as long as you name the
Service `postgres`.

## The four ways to expose something

This is the part people find confusing. There are three Service *types* plus
Ingress. Here they are from most private to most public.

### 1. ClusterIP — internal only (the default)

```yaml
spec:
  type: ClusterIP     # this is the default; you can leave it out
```

Reachable **only from inside the cluster**. Nothing from the outside world
can connect.

This is an **internal phone extension**. Other tenants can dial it. People on
the street cannot.

Use it for: databases, caches, and internal APIs. In our project, Postgres
and Redis are ClusterIP. This is the same decision as "no `ports:` in the
Compose file".

**Default to this.** Only change it if you have a reason.

To reach a ClusterIP from your laptop temporarily, forward a port:

```bash
kubectl port-forward svc/api 8080:80
```

### 2. NodePort — a fixed door on every node

```yaml
spec:
  type: NodePort
  ports:
    - port: 80
      targetPort: 8080
      nodePort: 30080     # must be 30000-32767
```

Kubernetes opens the **same high port number on every node**. Any node's IP
plus that port reaches your Service.

This is like putting **the same numbered side door on every building**, and
telling visitors "enter door 30080 at any building".

It works, but it is ugly: the port numbers are strange, you must know a node
IP, and there is no HTTPS or hostname routing. Use it for local testing and
demos, rarely in production.

### 3. LoadBalancer — a real public address

```yaml
spec:
  type: LoadBalancer
```

Kubernetes asks your **cloud provider** to create a real load balancer (an
AWS NLB/ELB, a GCP load balancer) with a real public IP.

This is a **proper street address** for the building.

The catch is money and scale: each LoadBalancer Service creates **its own**
cloud load balancer, and each one costs money every month. With 20
microservices, you get 20 load balancers and 20 bills.

On a local `kind` or `minikube` cluster, there is no cloud provider, so a
LoadBalancer Service stays `<pending>` forever. That is normal.

### 4. Ingress — one door, many destinations

Instead of one load balancer per service, you create **one** entry point and
route by **hostname and path**.

```
                    internet
                        │
                        ▼
             ┌────────────────────┐
             │  Ingress Controller │   one load balancer, one bill
             │   (nginx, traefik)  │
             └──────────┬─────────┘
                        │ reads Host: and path
        ┌───────────────┼────────────────┐
        ▼               ▼                ▼
   /api → Service   /web → Service   /admin → Service
```

This is the **receptionist at the front door**. One door for the whole
building. The receptionist reads your visitor form and says "third floor,
room 12".

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api
spec:
  ingressClassName: nginx
  rules:
    - host: api.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: api
                port:
                  number: 80
```

Because Ingress understands HTTP, it can also do things a plain Service
cannot:

- terminate **HTTPS/TLS** in one place, with certificates
- route `/api` to one service and `/web` to another
- route by hostname: `api.example.com` vs `admin.example.com`

### One thing that surprises everyone

**Creating an Ingress object does nothing by itself.**

An Ingress is only a *description* of routing rules. Something must read
those rules and actually move traffic. That something is an **Ingress
controller** — a real program (nginx, Traefik, HAProxy) that you must
install separately.

Writing an Ingress with no controller installed is like writing a delivery
address on a parcel and never giving it to a courier. Nothing moves.

In [lesson 6](06-deploy-to-kind.md) we install the nginx controller into
kind before applying any Ingress.

## Comparison table

| Type | Who can reach it | Cost | Use for |
|---|---|---|---|
| **ClusterIP** | Only inside the cluster | Free | Databases, internal APIs (**default**) |
| **NodePort** | Anyone who can reach a node IP | Free | Local testing |
| **LoadBalancer** | The internet | One cloud LB per service | A single public entry point |
| **Ingress** | The internet | One cloud LB total | Many HTTP services (**normal production choice**) |

The usual production shape is: **one Ingress** at the edge, and **everything
else ClusterIP** behind it.

## Special case: headless Services

One more type you will meet in [lesson 12](12-statefulsets.md):

```yaml
spec:
  clusterIP: None      # "headless"
```

A normal Service gives you one address and hides the Pods behind it. A
**headless** Service does the opposite: DNS returns the address of **every
individual Pod**.

You want this when each Pod is genuinely different and you must reach one
specific Pod — for example, the primary node of a database cluster. This is
why StatefulSets use headless Services.

## What our project uses

| Service | Type | Why |
|---|---|---|
| `api` | ClusterIP + Ingress | Public through one front door |
| `postgres` | ClusterIP (headless) | Internal only, stable per-pod names |
| `redis` | ClusterIP | Internal only |

Exactly the same reasoning as the Compose file: only the API is reachable
from outside; the datastores are not.

## Debugging a Service that does not work

Ninety percent of "my Service does not work" problems are a **label typo**.

```bash
kubectl get endpoints api
```

**This is the command to remember.** `Endpoints` lists the Pod IPs the
Service currently points to.

- If you see IP addresses → the Service is fine, the problem is elsewhere.
- If you see `<none>` → **no Pods match your selector**, or no Pod is Ready.

Then check the two possible reasons:

```bash
kubectl get pods --show-labels          # do the labels really match?
kubectl describe svc api                # what selector is the Service using?
kubectl get pods                        # is any pod actually READY 1/1?
```

Compare the Service's `Selector:` line with the Pod's labels, character by
character. `app: api` and `app: API` do not match.

Next: [ConfigMaps, Secrets, and Namespaces](04-config-and-secrets.md).
