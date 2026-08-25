# 3. Multi-Stage Builds for Small Go Images

## The problem a single-stage build has

Go compiles to a self-contained binary — there's no runtime interpreter and
no dependency tree to ship. But the *toolchain that builds it* is huge: the
`golang:1.24-bookworm` image is roughly 800MB+ once modules are downloaded,
because it carries the compiler, linker, standard library sources, and a
full Debian userland.

If you `FROM golang:1.24-bookworm` and stop there, every one of those build
tools ships in your runtime image too — a compiler your API never touches
in production, sitting in the image as both dead weight and attack surface.

## The fix: more than one `FROM`

A multi-stage Dockerfile has multiple `FROM` instructions, each starting a
new, independent stage. `COPY --from=<stage>` pulls specific files out of an
earlier stage into a later one — nothing else about that earlier stage
comes along. Only the *last* stage becomes the final image.

```dockerfile
# ---- build stage ----
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/api .

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/api /app/api
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
```

The `build` stage has the compiler, the module cache, every `.go` file, and
gigabytes of toolchain. None of it exists in the final image — only the one
file named after `--from=build`, the compiled `/out/api` binary, gets
copied across. This is exactly [`app/Dockerfile`](app/Dockerfile).

Check the size difference yourself once you've built the image:

```bash
docker images docker-crash-course-api
docker run --rm -it --entrypoint sh golang:1.24-bookworm -c \
  'echo "the build stage alone is this big before we even discard it"'
```

A single-stage equivalent of this image would be 800MB+; the actual
distroless-based image comes in under 20MB.

## Why `CGO_ENABLED=0` is what makes this possible

By default Go's build can link against the host's C library (cgo) for
things like DNS resolution or `net`/`os/user` on some platforms. A cgo
binary is dynamically linked against `libc` — it needs that library present
at runtime, which means the final image needs at least a `libc` (Alpine's
`musl` or Debian's `glibc`).

`CGO_ENABLED=0` forces a fully static binary with **no external library
dependencies at all**. That's the difference that lets the runtime stage be
`distroless/static` (`libc`-free) or even `scratch` (literally empty)
instead of needing `alpine` or `debian-slim` as a floor. Both drivers used
in [`app/`](app/) — `lib/pq` and `go-redis`Redis — are pure Go, so nothing
in this project needs cgo.

`GOOS=linux GOARCH=amd64` pins the target platform explicitly. This matters
if you're building on an Apple Silicon Mac (`darwin/arm64`) for an `amd64`
Linux host — without it, Go defaults to your host's OS/arch and the binary
won't run in the container at all. (For multi-architecture images — serving
both `arm64` and `amd64` from the same tag — see `docker buildx build
--platform linux/amd64,linux/arm64`, out of scope for this crash course.)

## Why `-trimpath -ldflags="-s -w"`

- **`-trimpath`** strips absolute build-machine file paths from the
  compiled binary (they'd otherwise show up in panics/stack traces),
  keeping the artifact reproducible and not leaking local filesystem
  layout.
- **`-ldflags="-s -w"`** drops the symbol table (`-s`) and DWARF debug
  info (`-w`), shrinking the binary — you lose `delve`-style debugging
  against the production binary, which is the right trade for a shipped
  artifact (debug against a locally built binary with symbols instead).

## Multiple build stages beyond just "build vs. runtime"

Two `FROM`s is the minimum useful shape, but stages can do more:

```dockerfile
FROM golang:1.24-bookworm AS base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM base AS lint
RUN go install honnef.co/go/tools/cmd/staticcheck@latest
COPY . .
RUN staticcheck ./...

FROM base AS test
COPY . .
RUN go test ./...

FROM base AS build
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api .

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/api /app/api
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
```

`docker build --target test` runs only up through the `test` stage — useful
for running the same Dockerfile in CI as both a test gate and an image
build, sharing the exact same dependency layer between them. Stages that
aren't reachable from the final `FROM` (like `lint` and `test` here) are
never pulled into the shipped image regardless — only `COPY --from`
reachability determines what ends up in the last stage.

Next: [Docker networking basics](04-networking-basics.md).
