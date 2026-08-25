# 4. Docker Networking Basics

## The default: bridge networking

When the Docker daemon starts, it creates a virtual bridge interface on the
host (`docker0`) — a software switch. Every container attached to the
default bridge gets its own network namespace (see
[lesson 1](01-containers-vs-vms.md)) with a virtual ethernet (`veth`) pair:
one end lives inside the container's namespace as `eth0`, the other end
plugs into the bridge on the host side. The bridge forwards traffic between
attached containers and, via NAT, out to the host's real network.

```
Host
┌───────────────────────────────────────────────┐
│                                                   │
│   docker0 (bridge, e.g. 172.17.0.1)              │
│     ├── veth ── eth0  container A (172.17.0.2)   │
│     └── veth ── eth0  container B (172.17.0.3)   │
│                                                   │
│   eth0 (host's real NIC) ── NAT ── internet       │
└───────────────────────────────────────────────┘
```

Containers on the same bridge can reach each other directly by IP. They can
reach the outside world through NAT on the host's real interface. Nothing
outside the host can reach *into* a container unless you explicitly publish
a port.

## The default bridge vs. a user-defined bridge

Docker ships a default bridge network (literally named `bridge`), but it has
a real limitation worth knowing: containers on it can only reach each other
by IP address — there's no automatic DNS between them.

```bash
docker network create demo
docker run -d --name api --network demo alpine sleep 3600
docker run -d --name redis --network demo redis:7-alpine
docker exec api ping -c1 redis   # resolves — user-defined bridge has embedded DNS
```

A **user-defined bridge** (`docker network create ...`, or whatever Compose
creates for you automatically) gets an embedded DNS server that resolves
container/service names to their current IP. This is *why*
[`app/`](app/) connects to `postgres:5432` and `redis:6379` by hostname
instead of a hardcoded IP — those hostnames are the service names Compose
registered on the `backend` network in
[`docker-compose.yml`](docker-compose.yml), and Compose always creates a
user-defined bridge, never uses the legacy default one.

## Publishing ports: `-p host:container`

By default, a container's ports are only reachable from other containers on
the same network — nothing on the host, and nothing outside it, can connect
in. `-p`/`--publish` opens a specific hole through NAT for that one port:

```bash
docker run -p 8080:8080 docker-crash-course-api:dev
#           ^host  ^container
```

This means: "traffic hitting the host on 8080, forward it to port 8080
inside this container." The two numbers don't have to match —
`-p 3000:8080` is legal and means the app still listens on 8080 *inside*
the container, but you reach it via `localhost:3000` from the host.

`EXPOSE 8080` in a Dockerfile (see
[`app/Dockerfile`](app/Dockerfile)) is **documentation only** — it doesn't
publish anything by itself. It tells a reader (and `docker network`
tooling) which port the process listens on; you still need `-p` (or
Compose's `ports:`) to actually make it reachable from outside the
container's network.

## What this project actually does

Look at [`docker-compose.yml`](docker-compose.yml):

```yaml
services:
  api:
    ports:
      - "8080:8080"        # published to the host — you curl this
    networks:
      - backend

  postgres:
    # no ports: — reachable only from other containers on `backend`
    networks:
      - backend

  redis:
    # same — no ports:
    networks:
      - backend
```

Only `api` publishes a port to the host. `postgres` and `redis` are
deliberately *not* published — nothing outside the `backend` network can
reach them directly, including nothing on your laptop's `localhost`. The
API reaches them because it's on the same user-defined bridge and can
resolve `postgres` / `redis` by name via that network's embedded DNS. This
is the same shape you want in production: only the service that needs to
be internet- or LB-facing gets a published/exposed port; datastores stay
reachable only from inside the network they share with the services that
need them.

## Quick diagnostics

```bash
docker network ls                       # networks on this host
docker network inspect docker-crash-course_backend
docker compose exec api sh -c 'nslookup postgres'   # confirm DNS on the bridge
docker port <container>                 # what's published, to where
```

Next: [docker-compose for a local stack](05-docker-compose-stack.md).
