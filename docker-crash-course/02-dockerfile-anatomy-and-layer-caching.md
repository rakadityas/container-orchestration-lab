# 2. Dockerfile Anatomy & Layer Caching

## Every instruction is a layer

Each instruction in a Dockerfile that changes filesystem state (`RUN`,
`COPY`, `ADD`) produces a new, immutable, content-addressed layer stacked on
the one before it. Metadata-only instructions (`ENV`, `EXPOSE`, `USER`,
`WORKDIR`, `ENTRYPOINT`, `CMD`, `ARG`) still create a layer in the image
history, but it carries no filesystem diff.

```dockerfile
FROM golang:1.24-bookworm   # layer: base image
WORKDIR /src                 # layer: metadata only
COPY go.mod go.sum ./        # layer: filesystem diff (2 files added)
RUN go mod download          # layer: filesystem diff (module cache populated)
COPY . .                     # layer: filesystem diff (source added)
RUN go build -o /out/api .   # layer: filesystem diff (binary added)
```

## The build cache: instructions are cached by layer, in order

For each instruction, the builder checks whether it already has a cached
layer for "this exact instruction, applied to this exact parent layer." If
yes, it reuses the cached layer instead of re-executing anything. The moment
one instruction misses the cache, **every instruction after it also
misses** — even if a later instruction would otherwise have produced an
identical result — because its parent layer is now different.

Cache keys work differently for the two families of instruction:

- **`RUN`**: cached by the literal command string. `RUN go build .` and
  `RUN go build .  ` (trailing space) are different cache keys.
- **`COPY` / `ADD`**: cached by the *content* of the files being copied
  (checksums), not just the command text. Change one byte in a copied file
  and that layer — and everything after it — invalidates, even though the
  `COPY` instruction's text didn't change.

## Why instruction order is the whole game

This is the single highest-leverage thing to get right in a Dockerfile:
**put what changes least often first, and what changes most often last.**

Compare two orderings of the same Go build:

```dockerfile
# ❌ source code changes bust the dependency-download layer every time
FROM golang:1.24-bookworm
WORKDIR /src
COPY . .
RUN go mod download
RUN go build -o /out/api .
```

```dockerfile
# ✅ dependency layer only rebuilds when go.mod/go.sum change
FROM golang:1.24-bookworm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/api .
```

In the first version, editing a single `.go` file invalidates `COPY . .`,
which invalidates `go mod download` right after it — so every source change
redownloads the entire module graph. In the second version (this is exactly
what [`app/Dockerfile`](app/Dockerfile) does), the dependency manifests are
copied and downloaded *before* the rest of the source, so editing
`main.go` only busts the cache from `COPY . .` onward — `go mod download`
stays cached and skipped.

This generalizes: package manager manifests before source
(`package.json`/`package-lock.json`, `requirements.txt`, `go.mod`/`go.sum`,
`Cargo.toml`/`Cargo.lock`), install/download before `COPY . .`, and anything
genuinely static (OS packages, fixed config) as early as possible.

## `.dockerignore` shapes the cache too

`COPY . .` sends the *build context* — the whole directory tree it draws
from — and its cache key is a hash of that context's contents. Files that
change on every build but never affect the artifact (`.git/`, editor swap
files, local `.env`s, previous build output) will bust the cache for no
reason if they're included. [`app/.dockerignore`](app/.dockerignore)
excludes exactly that class of file — see [lesson 6](06-image-security.md)
for why this also matters for *security*, not just cache hits: an
`.dockerignore` miss is how `.env` files and `.git` history end up baked
into a shipped image.

## `ARG` vs `ENV`

- **`ARG`** — a build-time-only variable. Available during the build, gone
  at runtime, not present in the final image's environment. Use it for
  things like a Go version or a target platform.
- **`ENV`** — persists into the running container's environment, and into
  every subsequent layer's build environment too. Use it for values the
  *application* needs at runtime (a default `PORT`, for example).

```dockerfile
ARG GO_VERSION=1.24
FROM golang:${GO_VERSION}-bookworm AS build
...
ENV PORT=8080
```

Don't put secrets in either — `ARG` values are visible in `docker history`
and cached layer metadata even though they don't ship in the final image's
environment. Use `--secret` with BuildKit for anything sensitive a build
step needs to read (an npm token, a private-module credential).

## `CMD` vs `ENTRYPOINT`

- **`ENTRYPOINT`** is the fixed executable the container runs.
- **`CMD`** supplies default *arguments* to it (or, with no `ENTRYPOINT`,
  is itself the command) — and is overridden wholesale by anything passed
  after `docker run <image>`.

[`app/Dockerfile`](app/Dockerfile) uses only `ENTRYPOINT ["/app/api"]`
because the API binary has nothing worth defaulting or overriding — it just
runs. A CLI tool with commonly-overridden flags is the more typical case
for splitting the two, e.g. `ENTRYPOINT ["myctl"]` + `CMD ["--help"]`.

## Read the Dockerfile now

Open [`app/Dockerfile`](app/Dockerfile) and match every instruction to a
rule above: dependency files copied and downloaded before source, source
copied after, `.dockerignore` trimming the context. The `ldflags`/`trimpath`
build flags and the second `FROM` are covered next, in
[multi-stage builds](03-multistage-builds-go.md).
