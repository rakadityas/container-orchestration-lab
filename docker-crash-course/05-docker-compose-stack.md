# 5. docker-compose for a Local Multi-Service Stack

## The big idea, in simple words

`docker run` is like giving instructions to **one worker at a time**. You
must remember every option, type it correctly, and repeat it for every
container.

**Compose** is like writing one **recipe** for the whole kitchen. You write
down all the services once, in one file. Then one command starts everything,
in the right order, connected correctly. Another command stops it all.

[`docker-compose.yml`](docker-compose.yml) in this project describes three
services: `api`, `postgres`, and `redis`.

## The commands you will use

```bash
docker compose up --build     # build the api image, start all three, show logs
docker compose up -d          # same, but run in the background
docker compose ps             # show what is running
docker compose logs -f api    # watch the logs of one service only
docker compose down           # stop and delete the containers and the network
docker compose down -v        # also delete the pgdata volume (ERASES the database)
```

Be careful with the last one. The `-v` flag deletes your database data.

## Reading the file, part by part

### `build:` vs `image:`

```yaml
api:
  build:
    context: ./app
    dockerfile: Dockerfile
  image: docker-crash-course-api:dev
```

`postgres` and `redis` use only `image:`. This means: download a ready-made
image from the internet and run it as it is.

`api` is our own code, so no ready-made image exists. `build:` tells Compose
to build it from [`app/Dockerfile`](app/Dockerfile), using the `./app`
folder as the build context (see
[lesson 2](02-dockerfile-anatomy-and-layer-caching.md) for why
`.dockerignore` matters here).

Adding `image:` as well simply gives the result a readable name, so
`docker images` shows `docker-crash-course-api:dev` instead of a random
string of letters and numbers.

### `environment:` — how the API finds the database

```yaml
environment:
  DATABASE_URL: postgres://postgres:postgres@postgres:5432/appdb?sslmode=disable
  REDIS_ADDR: redis:6379
```

Compare this with [`app/main.go`](app/main.go), which reads
`os.Getenv("DATABASE_URL")` and `os.Getenv("REDIS_ADDR")`.

The host names `postgres` and `redis` are exactly the service names written
in this same file. They work because Compose put all three services on one
user-defined bridge network with a phone book (see
[lesson 4](04-networking-basics.md)).

Writing the password directly in this file is acceptable for a local
learning project. **Do not do this in a real deployment.** There, the values
should come from a secrets manager, or from a `.env` file that is never
committed to git (see [lesson 6](06-image-security.md)).

### `depends_on` + `healthcheck` — waiting correctly

This part solves a real and very common bug.

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

**The problem.** A plain `depends_on: [postgres]` only waits for the
container to **start**. It does not wait for Postgres inside the container to
finish preparing itself and become ready to accept connections.

Imagine arriving at a restaurant. The lights are on and the door is open, so
the restaurant "started". But the kitchen is still warming up, and nobody
can take your order yet. If you order immediately, you get an error.

This is a **race condition**: two things start at the same time, and
sometimes one finishes first, sometimes the other. The bug appears randomly.
It usually works on your laptop (because you start things slowly, by hand)
and then fails in CI, where everything starts at once.

**The solution.** A `healthcheck` is a small command that the container runs
again and again to answer one question: "am I really ready?"

- Postgres uses `pg_isready`
- Redis uses `redis-cli ping`

Then `condition: service_healthy` tells Compose: do not start `api` until
that check passes. Now the waiter only comes when the kitchen is truly
ready.

The application defends itself too. [`app/main.go`](app/main.go) stops
immediately with `log.Fatalf` if it cannot connect at startup. It also
offers two different endpoints, and the difference matters:

| Endpoint | Question it answers |
|---|---|
| `/healthz` | Is the process alive? (**liveness**) |
| `/readyz` | Can it actually reach Postgres and Redis? (**readiness**) |

Kubernetes uses this same split. "The program is running" and "the program
can do useful work" are two different things, and mixing them causes
restarts at the wrong moment.

Note that the `api` service itself has **no** `healthcheck:` block in this
project. Compose therefore does not check `/healthz` automatically — nothing
calls that endpoint unless you call it yourself, or a load balancer or
Kubernetes calls it. Adding one is harder here than it looks, because the
distroless image has no shell and no `curl` to run a check with (see
[lesson 6](06-image-security.md)).

### `volumes:` — keeping data after the container is gone

```yaml
postgres:
  volumes:
    - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

Remember from [lesson 1](01-containers-vs-vms.md): the writable layer of a
container is deleted together with the container.

Postgres keeps its real data files in `/var/lib/postgresql/data`. Without a
volume, `docker compose down` would quietly erase your whole database.

A **named volume** like `pgdata` is storage managed by Docker or Podman
itself. It is not part of any container, so it survives
`docker compose down`. It is deleted only if you explicitly add `-v`.

Think of a volume as an **external hard drive**. You can throw the computer
away; the hard drive keeps your files.

`redis` has no volume here on purpose. Its data (the hit counters from
`/items/{id}/hit`) is just a cache. Losing it is acceptable.

### `networks:` — the shared bridge

```yaml
networks:
  backend:
    driver: bridge
```

All three services list `networks: [backend]`. This is the user-defined
bridge from [lesson 4](04-networking-basics.md) that gives them the name
phone book.

Compose would create a similar network automatically even without this
block. It is written here so the network has a clear name and is visible in
the file, instead of being hidden.

## Try it: break the ordering on purpose

The best way to understand `service_healthy` is to remove it and watch the
failure.

1. Run `docker compose down -v` so Postgres must do first-time setup again
   (this takes longer, which makes the race easier to see).
2. Comment out the `condition: service_healthy` lines.
3. Run `docker compose up --build` and read the `api` logs.

You should see connection errors, because the API tried to connect before
Postgres was ready. Then put the lines back and run it again. The errors
disappear.

Next: [Image security](06-image-security.md).
