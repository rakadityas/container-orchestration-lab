# 3. Multi-Stage Builds for Small Go Images

## The big idea, in simple words

Think about baking a cake.

To bake it you need a big kitchen: an oven, bowls, flour, sugar, a mixer,
and you make a big mess. But when you serve the cake to your guests, you
only carry **the cake** to the table. You do not carry the oven, the flour
bag, and the dirty bowls to the table too.

A **multi-stage build** does exactly this. The first stage is the messy
kitchen where your program is compiled. The last stage is the clean plate
that holds only the finished program.

Result: your final image goes from about **800 MB** down to under **20 MB**.

## The problem with a single-stage build

Go compiles into one self-contained file. There is no interpreter to ship
and no library folder to ship.

But the *tools that build it* are very large. The `golang:1.24-bookworm`
image is around 800 MB or more after downloading libraries. It contains the
compiler, the linker, the standard library source code, and a full Debian
system.

If you write `FROM golang:1.24-bookworm` and stop there, all of those tools
are shipped inside your final image. Your API never uses a compiler in
production. So the compiler is two bad things at once: wasted space, and
extra tools that an attacker could use.

## The fix: use more than one `FROM`

A multi-stage Dockerfile has several `FROM` lines. Each `FROM` starts a new,
separate stage. `COPY --from=<stage>` takes specific files out of an earlier
stage. Nothing else from that stage is carried over.

**Only the last stage becomes your final image.**

```dockerfile
# ---- build stage (the messy kitchen) ----
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/api .

# ---- runtime stage (the clean plate) ----
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/api /app/api
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
```

The `build` stage holds the compiler, the downloaded libraries, every `.go`
file, and gigabytes of tools. **None of it exists in the final image.** Only
one single file crosses the line: the compiled program `/out/api`, taken by
`COPY --from=build`.

This is exactly what [`app/Dockerfile`](app/Dockerfile) does.

Check the size yourself after you build:

```bash
docker images docker-crash-course-api
```

## Why `CGO_ENABLED=0` is the key that makes this work

Normally, Go can connect your program to the C library on the system (this
feature is called cgo). A program built that way is **dynamically linked**.
It needs that C library file to exist at runtime. So the final image must
contain at least a C library, which means you need `alpine` or
`debian-slim` as a minimum.

`CGO_ENABLED=0` forces Go to build a **fully static** program with **no
external library needed at all**. Everything is inside the one file.

Think of it like the difference between a **puzzle that needs a missing
piece from another box** and a **single solid brick**. The solid brick works
anywhere.

This is what allows the final stage to be `distroless/static` (no C library)
or even `scratch` (a completely empty image). Both database drivers used in
[`app/`](app/) — `lib/pq` for Postgres and `go-redis` for Redis — are
written in pure Go, so this project never needs cgo.

## Why `GOOS` and `GOARCH` matter (and why you should not hardcode them)

`GOOS` is the target operating system. `GOARCH` is the target CPU type.

The two common CPU types today are:

- `amd64` — Intel and AMD processors (most servers, older Macs, most PCs)
- `arm64` — Apple Silicon Macs (M1, M2, M3, M4), AWS Graviton servers

A program built for one CPU type **cannot run** on the other. If the types
do not match, Docker tries to translate every instruction using an emulator
called QEMU. This is very slow, and Go programs often **crash** under it
with a confusing `SIGSEGV: segmentation violation` error.

The safe solution is to never write the CPU type by hand. Docker provides
these values automatically:

| Variable | Meaning |
|---|---|
| `BUILDPLATFORM` | The machine doing the building (your laptop) |
| `TARGETOS` | The operating system the image is for |
| `TARGETARCH` | The CPU type the image is for |

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/api .
```

Read this in two parts:

1. `--platform=$BUILDPLATFORM` tells Docker: **run the compiler natively**,
   at full speed, with no emulator.
2. `GOOS=$TARGETOS GOARCH=$TARGETARCH` tells Go: **build the output for the
   target machine**. Go can cross-compile very well on its own, so it does
   not need an emulator to do this.

Together they mean: compile fast on the machine you have, and produce a
program for the machine you want.

To build one image that works on both CPU types, use:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t myimage:tag ./app
```

## Why `-trimpath` and `-ldflags="-s -w"`

These two flags make the compiled program smaller and safer.

- **`-trimpath`** removes the full folder paths from your build machine.
  Without it, a crash message could show something like
  `/Users/yourname/secret-project/main.go`. This leaks information about
  your computer and also makes builds harder to reproduce.
- **`-ldflags="-s -w"`** removes the symbol table (`-s`) and the debug
  information (`-w`). This makes the file noticeably smaller.

The cost of `-s -w` is that you cannot attach a debugger like `delve` to the
production program. This is the right trade: debug a locally built version
that still has the debug information instead.

## More than two stages

Two `FROM` lines is the smallest useful version. But stages can do more than
"build" and "run".

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

Here all stages share one `base` stage, so the libraries are downloaded only
one time for all of them.

```bash
docker build --target test ./app     # stop after the test stage
```

This runs only up to the `test` stage. Now CI can use the same Dockerfile
for testing and for building the image.

Note that `lint` and `test` never reach the final image. The last stage
never copies from them, and only what the last stage copies is shipped.

Next: [Docker networking basics](04-networking-basics.md).
