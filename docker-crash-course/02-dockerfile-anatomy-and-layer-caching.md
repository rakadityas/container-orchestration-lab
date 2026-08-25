# 2. Dockerfile Anatomy & Layer Caching

## The big idea, in simple words

Think of a Dockerfile like a **tower of blocks**. Each line in the file adds
one block on top of the tower.

Docker is lazy, and this is good. If you build the same tower again, Docker
says: "I already made this block last time. I will reuse it." This is called
the **cache**, and it makes builds very fast.

But there is one rule you must remember:

> If you change a block near the **bottom**, every block **above** it falls
> down and must be built again.

So the whole skill of writing a good Dockerfile is simple: **put the things
that rarely change at the bottom, and the things that change often at the
top.**

## Every instruction creates a layer

Some instructions change files (`RUN`, `COPY`, `ADD`). Each of these creates
a new layer. A layer is permanent and cannot be changed later.

Other instructions only set information (`ENV`, `EXPOSE`, `USER`,
`WORKDIR`, `ENTRYPOINT`, `CMD`, `ARG`). They also appear in the image
history, but they add no files.

```dockerfile
FROM golang:1.24-bookworm   # layer: the base image
WORKDIR /src                 # layer: information only, no files
COPY go.mod go.sum ./        # layer: 2 files added
RUN go mod download          # layer: downloaded libraries added
COPY . .                     # layer: source code added
RUN go build -o /out/api .   # layer: the compiled program added
```

## How the cache decides to reuse a layer

For each line, Docker asks one question: "Do I already have a layer for
*this exact instruction*, built on top of *this exact layer below it*?"

If the answer is yes, Docker reuses the old layer and runs nothing.

If the answer is no, Docker builds that layer again — **and every single
line after it is also built again**. This happens even if a later line would
have produced the same result. The reason is simple: the layer below it
changed, so it is no longer the same tower.

The two families of instruction are checked in different ways:

- **`RUN`** — checked by the **text of the command**. `RUN go build .` and
  `RUN go build .  ` (with extra spaces at the end) count as two different
  commands.
- **`COPY` / `ADD`** — checked by the **contents of the files** you copy,
  not by the text. If you change one letter inside a copied file, that layer
  and everything after it must be built again, even though the `COPY` line
  itself looks the same.

## Why the order of lines is everything

This is the most valuable thing to learn about Dockerfiles:
**put what changes least often first, and what changes most often last.**

Look at two versions of the same Go build.

```dockerfile
# ❌ BAD: changing any source file re-downloads all libraries
FROM golang:1.24-bookworm
WORKDIR /src
COPY . .
RUN go mod download
RUN go build -o /out/api .
```

```dockerfile
# ✅ GOOD: libraries are downloaded again only when go.mod/go.sum change
FROM golang:1.24-bookworm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/api .
```

In the **bad** version, you edit one line in one `.go` file. This changes
`COPY . .`, which sits *below* `go mod download`. So Docker must run
`go mod download` again and download every library from the internet. This
can take minutes, every single time you change one line of code.

In the **good** version, the files that list your libraries (`go.mod` and
`go.sum`) are copied first and downloaded first. They change rarely. So when
you edit `main.go`, only the lines from `COPY . .` downward run again. The
download stays cached and is skipped. This is exactly what
[`app/Dockerfile`](app/Dockerfile) does.

The same rule works in every language. Copy the file that lists your
libraries first, install them, and only then copy your source code:

| Language | Copy these first |
|---|---|
| Go | `go.mod`, `go.sum` |
| Node.js | `package.json`, `package-lock.json` |
| Python | `requirements.txt` |
| Rust | `Cargo.toml`, `Cargo.lock` |

Put anything truly fixed (operating system packages, static config) as early
as possible.

## `.dockerignore` also protects the cache

When you write `COPY . .`, Docker first sends the whole folder to the
builder. This folder is called the **build context**. The cache check uses
the contents of that folder.

Some files change on every build but have no effect on your program:
`.git/`, editor temporary files, local `.env` files, old build output. If
Docker can see them, they break your cache for no reason at all.

[`app/.dockerignore`](app/.dockerignore) lists exactly this kind of file so
Docker ignores them. This also protects you for a second reason: security.
See [lesson 6](06-image-security.md) — a missing `.dockerignore` line is how
`.env` files with real passwords, and full `.git` history, accidentally end
up inside a shipped image.

## `ARG` vs `ENV`

Both hold values, but they live for different amounts of time.

- **`ARG`** — exists **only while building**. It disappears when the build
  finishes and is not inside the final image. Use it for things like a
  version number or a target platform.
- **`ENV`** — stays **inside the running container**, and is also visible to
  every later line during the build. Use it for values your *application*
  reads when it runs, for example a default `PORT`.

```dockerfile
ARG GO_VERSION=1.24
FROM golang:${GO_VERSION}-bookworm AS build
...
ENV PORT=8080
```

**Never put passwords or secret keys in either one.** Even though `ARG`
values do not stay in the final image's environment, anyone can still read
them with `docker history`. If a build step truly needs a secret (an npm
token, a private library password), use BuildKit's `--secret` option
instead.

## `CMD` vs `ENTRYPOINT`

- **`ENTRYPOINT`** is the program the container always runs.
- **`CMD`** gives default *arguments* to that program. If there is no
  `ENTRYPOINT`, then `CMD` is the command itself. Anything you type after
  `docker run <image>` replaces `CMD` completely.

A simple way to remember it: `ENTRYPOINT` is the **car**, and `CMD` is the
**default destination** you can change.

[`app/Dockerfile`](app/Dockerfile) uses only `ENTRYPOINT ["/app/api"]`. The
API has no options worth changing — it simply runs. Splitting the two is
more useful for a command-line tool, for example
`ENTRYPOINT ["myctl"]` with `CMD ["--help"]`.

## Now read the real Dockerfile

Open [`app/Dockerfile`](app/Dockerfile). Try to match every line to a rule
above: library files copied and downloaded before the source code, source
code copied after, and `.dockerignore` keeping the context small.

The build flags (`-ldflags`, `-trimpath`) and the second `FROM` line are
explained next, in [multi-stage builds](03-multistage-builds-go.md).
