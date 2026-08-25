# Docker Crash Course

A hands-on course about containers: how they work inside, how to write a good
Dockerfile, and how to ship an image to production on AWS ECR.

Each lesson is short and uses simple language with everyday comparisons. The
goal is not only to know *what* to type, but to understand *why*.

The practice project is in [`app/`](app/): a small Go REST API that you
containerize with a multi-stage build and run together with Postgres and
Redis using [`docker-compose.yml`](docker-compose.yml).

## Lessons

| # | Lesson | Main idea in one line |
|---|---|---|
| 1 | [Containers vs VMs](01-containers-vs-vms.md) | A VM builds a new house; a container rents a room |
| 2 | [Dockerfile Anatomy & Layer Caching](02-dockerfile-anatomy-and-layer-caching.md) | A tower of blocks: change the bottom, rebuild everything above |
| 3 | [Multi-Stage Builds for Small Go Images](03-multistage-builds-go.md) | Bake in a messy kitchen, carry only the cake to the table |
| 4 | [Docker Networking Basics](04-networking-basics.md) | A private WiFi with a phone book, and doors you open on purpose |
| 5 | [docker-compose for a Local Stack](05-docker-compose-stack.md) | One recipe for the whole kitchen, and waiting until it is truly ready |
| 6 | [Image Security](06-image-security.md) | Ship a small box, without the master key and without your passport |
| 7 | [Shipping to AWS ECR](07-ecr-deployment.md) | Visitor badges, permanent ink labels, automatic cleanup |

## The practice project

[`app/`](app/) is a small Go REST API with four endpoints. It uses Postgres
for durable storage and Redis for a fast hit counter. It is big enough to
practice every lesson, and small enough to read in a few minutes.

| Endpoint | What it does |
|---|---|
| `GET /healthz` | Liveness: is the process alive? |
| `GET /readyz` | Readiness: can it reach Postgres and Redis? |
| `GET /items` | List items from Postgres |
| `POST /items` | Create an item in Postgres |
| `POST /items/{id}/hit` | Increase the Redis hit counter |

```
docker-crash-course/
├── app/                  # the Go service
│   ├── main.go
│   ├── internal/
│   │   ├── api/          # HTTP handlers
│   │   ├── db/           # Postgres
│   │   └── cache/        # Redis
│   ├── go.mod / go.sum
│   ├── Dockerfile        # multi-stage, distroless, non-root
│   ├── Makefile          # build and scan shortcuts
│   └── .dockerignore
├── ci/                   # example GitHub Actions pipeline (not active)
└── docker-compose.yml    # api + postgres + redis on a bridge network
```

## Run it

Docker and Podman use the same commands and the same Compose file. Use
whichever one you have installed.

```bash
cd docker-crash-course

# with Docker
docker compose up --build

# with Podman
podman compose up -d --build
```

Then, in another terminal:

```bash
# is it alive? is it ready?
curl localhost:8080/healthz
curl localhost:8080/readyz

# create an item (this writes to Postgres)
curl -X POST localhost:8080/items -d '{"name":"first item"}'

# list the items
curl localhost:8080/items

# increase the Redis hit counter for item 1
curl -X POST localhost:8080/items/1/hit
```

Stop everything:

```bash
docker compose down       # stop containers, keep the database
docker compose down -v    # also DELETE the database volume
```

## Suggested order

1. **Read lessons 1 to 4 first.** They explain *why* the Dockerfile and the
   Compose file look the way they do. Without this, the files look like
   magic.
2. **Then open [`app/Dockerfile`](app/Dockerfile) and
   [`docker-compose.yml`](docker-compose.yml) next to lessons 3, 5, and 6.**
   Those lessons point at specific lines in those files.
3. **Lesson 7 is reference.** Read it when you are ready to push an image to
   a real registry.

## A note about CPU types (Apple Silicon)

If you use a Mac with an M1, M2, M3, or M4 chip, your CPU is `arm64`. Most
cloud servers are `amd64`. A program built for one **cannot run** on the
other without a slow emulator, and Go programs often crash under that
emulator.

The Dockerfile in this project avoids the problem by never writing the CPU
type by hand. It uses Docker's automatic `TARGETARCH` value instead. See
[lesson 3](03-multistage-builds-go.md) for the full explanation.
