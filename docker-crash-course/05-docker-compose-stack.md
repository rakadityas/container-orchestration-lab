# 5. docker-compose for a Local Multi-Service Stack

## What Compose actually does

`docker run` describes one container at a time on the command line.
**Compose** describes a whole set of related services declaratively, in one
YAML file, and manages them as a unit: build/pull images, create a shared
network, start services in dependency order, wire up volumes, tear
everything down together.

[`docker-compose.yml`](docker-compose.yml) in this project defines three
services — `api`, `postgres`, `redis` — the same stack you'd otherwise wire
up with three separate `docker network create` / `docker run` invocations.

```bash
docker compose up --build     # build the api image, start all three, follow logs
docker compose up -d          # same, but detached
docker compose ps             # what's running
docker compose logs -f api    # tail just one service
docker compose down           # stop and remove containers + the network
docker compose down -v        # also remove the pgdata volume (wipes the DB)
```

## Reading the file, section by section

### `build:` vs `image:`

```yaml
api:
  build:
    context: ./app
    dockerfile: Dockerfile
  image: docker-crash-course-api:dev
```

`postgres` and `redis` use `image:` alone — pull a published image and run
it as-is. `api` has no published image to pull; `build:` tells Compose to
build it locally from [`app/Dockerfile`](app/Dockerfile), using `./app` as
the build context (everything `COPY` can see — see
[lesson 2](02-dockerfile-anatomy-and-layer-caching.md) on why
`.dockerignore` matters here too). Naming it with `image:` as well just
tags the result so `docker images` shows something readable instead of a
generated hash.

### `environment:` — how the API finds its dependencies

```yaml
environment:
  DATABASE_URL: postgres://postgres:postgres@postgres:5432/appdb?sslmode=disable
  REDIS_ADDR: redis:6379
```

Compare this to how [`app/main.go`](app/main.go) reads its configuration —
`os.Getenv("DATABASE_URL")`, `os.Getenv("REDIS_ADDR")`. The hostnames
`postgres` and `redis` here are exactly the service names defined in this
same file; that resolution only works because Compose put all three
services on one user-defined bridge network (see
[lesson 4](04-networking-basics.md)). Hardcoding credentials in plain YAML
like this is fine for a local learning stack; a real deployment would pull
these from a secrets manager or `.env` file excluded from version control
(see [lesson 6](06-image-security.md) on what belongs in `.dockerignore`
and out of images entirely).

### `depends_on` + `healthcheck` — ordering that's actually correct

```yaml
api:
  depends_on:
    postgres:
      condition: service_healthy
    redis:
      condition: service_healthy

postgres:
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U postgres -d appdb"]
    interval: 5s
    timeout: 5s
    retries: 10
```

Plain `depends_on: [postgres]` only waits for the *container* to start —
not for Postgres inside it to finish initializing and start accepting
connections. That gap is exactly the class of bug where a service works
locally (you started it slowly, by hand) and flakes in CI or on a fresh
`docker compose up` (everything races to start at once).
`condition: service_healthy` makes Compose wait for the dependency's
`healthcheck` to actually pass before starting `api` — `pg_isready` for
Postgres, `redis-cli ping` for Redis. [`app/main.go`](app/main.go) also
defends itself independently, failing fast with `log.Fatalf` if it can't
connect on startup, and exposes `/readyz` (distinct from `/healthz`) so an
orchestrator can tell "process is up" apart from "dependencies are
reachable" — the same liveness/readiness split Kubernetes expects.

### `volumes:` — surviving container removal

```yaml
postgres:
  volumes:
    - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

Recall from [lesson 1](01-containers-vs-vms.md): a container's writable
layer disappears with the container. Postgres's actual data files live at
`/var/lib/postgresql/data` inside the container — without a volume, `docker
compose down` (or any container recreation) would silently wipe the
database. The named volume `pgdata` is managed by the Docker/Podman engine
independently of any container's lifecycle; it survives `docker compose
down` and is only removed if you explicitly pass `-v`. `redis` in this
stack has no volume — its state (the hit counters from `/items/{id}/hit`)
is treated as disposable cache, which is the correct call for what it's
storing here.

### `networks:` — the shared bridge

```yaml
networks:
  backend:
    driver: bridge
```

All three services list `networks: [backend]`; this is the user-defined
bridge from [lesson 4](04-networking-basics.md) that gives them DNS
resolution by service name. Compose would create an equivalent default
network automatically even without this block — it's spelled out here so
the network's existence and name are visible in the file rather than
implicit.

## Try it: break the ordering on purpose

Comment out the `condition: service_healthy` depends_on block, force
Postgres to start slowly (`docker compose up --build --scale postgres=1`
after a fresh `down -v`, so it has to run first-time initialization), and
watch `api`'s logs — you should see the connection-refused failures that
`service_healthy` was preventing. Then restore it.

Next: [Image security](06-image-security.md).
