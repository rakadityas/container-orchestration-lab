# 4. Docker Networking Basics

## The big idea, in simple words

Think of a container network like a **private WiFi network inside a house**.

- Every container that joins this WiFi can talk to every other container on
  it.
- People **outside** the house cannot reach anything on this WiFi. The house
  has no open door yet.
- To let outside visitors reach one container, you must **open one specific
  door** for it. This is called publishing a port.

There is one more useful idea: a **phone book**. On a good network, you can
call a container by its **name** ("call postgres"). On the old default
network, there is no phone book, so you must know the exact **number** (the
IP address), and that number can change.

## The default: bridge networking

When Docker starts, it creates a virtual switch on your machine called
`docker0`. Think of it as the WiFi router in the story above.

Every container gets its own network namespace (see
[lesson 1](01-containers-vs-vms.md)) and a **virtual cable** with two ends,
called a `veth` pair:

- one end is inside the container and is named `eth0`
- the other end plugs into the `docker0` switch on the host

The switch passes traffic between the containers. It also lets them reach
the internet through the host's real network card, using NAT (network
address translation).

```
Host
┌──────────────────────────────────────────────────┐
│                                                  │
│   docker0 (bridge, e.g. 172.17.0.1)              │
│     ├── veth ── eth0  container A (172.17.0.2)   │
│     └── veth ── eth0  container B (172.17.0.3)   │
│                                                  │
│   eth0 (host's real network card) ── NAT ── internet │
└──────────────────────────────────────────────────┘
```

So:

- Containers on the same bridge can reach each other directly.
- They can reach the internet through the host.
- Nothing outside can reach *into* a container until you publish a port.

## The default bridge vs. a user-defined bridge

Docker has one built-in network simply named `bridge`. It works, but it has
one important limitation: **containers on it can only reach each other by IP
address**. There is no phone book.

A **user-defined bridge** is a network you create yourself. It includes a
small DNS server, which is the phone book. Now containers can find each
other by name.

```bash
docker network create demo
docker run -d --name api --network demo alpine sleep 3600
docker run -d --name redis --network demo redis:7-alpine
docker exec api ping -c1 redis   # works! the name "redis" is resolved
```

This is *why* the code in [`app/`](app/) connects to `postgres:5432` and
`redis:6379` using names instead of IP addresses. Those names are the
service names written in [`docker-compose.yml`](docker-compose.yml).

Good news: **Compose always creates a user-defined bridge for you.** It
never uses the old default one. So you always get the phone book.

## Publishing ports: `-p host:container`

By default, nobody outside the container network can connect to your
container — not even you, from your own laptop. The `-p` option (also
written `--publish`) opens one door:

```bash
docker run -p 8080:8080 docker-crash-course-api:dev
#           ^host  ^container
```

Read it as: "traffic arriving at the **host** on port 8080, send it to port
8080 **inside** the container."

The left number is outside. The right number is inside. **They do not have
to match.**

```bash
docker run -p 3000:8080 docker-crash-course-api:dev
```

Here the application still listens on port 8080 inside the container, but
you open `localhost:3000` in your browser.

### `EXPOSE` does not open anything

`EXPOSE 8080` in a Dockerfile (see [`app/Dockerfile`](app/Dockerfile)) is
**only a note for humans and tools**. It publishes nothing.

Think of `EXPOSE` as a **label on a door** that says "port 8080 is behind
here". The label does not unlock the door. You still need `-p` (or `ports:`
in Compose) to actually open it.

## What this project does

Look at [`docker-compose.yml`](docker-compose.yml):

```yaml
services:
  api:
    ports:
      - "8080:8080"        # open to the host — you can curl this
    networks:
      - backend

  postgres:
    # no ports: — only containers on `backend` can reach it
    networks:
      - backend

  redis:
    # same — no ports:
    networks:
      - backend
```

Only `api` opens a door to the host. `postgres` and `redis` open **no
doors** on purpose. Nothing outside the `backend` network can reach them —
not even a program running directly on your laptop.

The API can still reach them because it sits on the same network and can
look up the names `postgres` and `redis` in the phone book.

This is the same shape you want in production:

> Only the service that must face the internet gets a published port.
> Databases stay reachable **only** from inside the network.

## Useful commands for checking your network

```bash
docker network ls                       # list all networks
docker network inspect docker-crash-course_backend
docker compose exec api sh -c 'nslookup postgres'   # test the phone book
docker port <container>                 # show which doors are open
```

Next: [docker-compose for a local stack](05-docker-compose-stack.md).
