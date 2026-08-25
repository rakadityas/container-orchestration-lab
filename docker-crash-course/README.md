# Docker Crash Course

A hands-on curriculum covering container internals, Dockerfile best practices,
and shipping an image to production (ECR). Each numbered lesson is a short
read; the payoff is the working project in [`app/`](app/) — a Go REST API
you containerize with a multi-stage build and run alongside Postgres and
Redis via [`docker-compose.yml`](docker-compose.yml).

## Lessons

1. [Containers vs VMs](01-containers-vs-vms.md) — namespaces, cgroups, union filesystems
2. [Dockerfile Anatomy & Layer Caching](02-dockerfile-anatomy-and-layer-caching.md)
3. [Multi-Stage Builds for Small Go Images](03-multistage-builds-go.md)
4. [Docker Networking Basics](04-networking-basics.md) — bridge networks, port mapping
5. [docker-compose for a Local Stack](05-docker-compose-stack.md) — Go API + Postgres + Redis
6. [Image Security](06-image-security.md) — distroless, non-root, Trivy, `.dockerignore`, immutable tags
7. [Shipping to AWS ECR](07-ecr-deployment.md) — registry auth, tag immutability, lifecycle policies

## The hands-on project

[`app/`](app/) is a small Go REST API (`/healthz`, `/readyz`, `/items`,
`/items/{id}/hit`) that talks to Postgres (durable storage) and Redis (a hit
counter) — enough surface area to exercise every lesson above without being
a toy `hello world`.

```
courses/docker-crash-course/
├── app/                  # the Go service
│   ├── main.go
│   ├── internal/
│   │   ├── api/          # HTTP handlers
│   │   ├── db/           # Postgres
│   │   └── cache/        # Redis
│   ├── go.mod / go.sum
│   ├── Dockerfile        # multi-stage, distroless, non-root
│   └── .dockerignore
└── docker-compose.yml    # api + postgres + redis on a bridge network
```

### Run it (Docker or Podman)

Both speak the same CLI and Compose file — swap `docker` for `podman` (and
`docker compose` for `podman compose`, or `podman-compose`) in every command
below if that's your engine.

```bash
cd docker-crash-course
docker compose up --build
```

Then, in another terminal:

```bash
# liveness / readiness
curl localhost:8080/healthz
curl localhost:8080/readyz

# create an item (hits Postgres)
curl -X POST localhost:8080/items -d '{"name":"first item"}'

# list items
curl localhost:8080/items

# bump the Redis-backed hit counter for item 1
curl -X POST localhost:8080/items/1/hit
```

Tear down (and drop the Postgres volume) with:

```bash
docker compose down -v
```

### Suggested order

Read lessons 1–4 first — they explain *why* the Dockerfile and compose file
look the way they do. Then open [`app/Dockerfile`](app/Dockerfile) and
[`docker-compose.yml`](docker-compose.yml) side by side with lessons 3, 5,
and 6, which point at specific lines. Lesson 7 is standalone reference for
when you're ready to push an image somewhere real.
