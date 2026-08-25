# syntax=docker/dockerfile:1

# ---- build stage ---------------------------------------------------------
# Pin the exact toolchain version, not `golang:latest` — a floating tag means
# your build silently changes out from under you.
#
# --platform=$BUILDPLATFORM runs the builder natively (no QEMU emulation)
# even when cross-compiling for a different TARGETARCH below: Go's own
# GOOS/GOARCH cross-compilation handles that without needing the toolchain
# itself to run under emulation.
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build

WORKDIR /src

# Copy only dependency manifests first so `go mod download` is cached in its
# own layer and only reruns when go.mod/go.sum actually change.
COPY go.mod go.sum ./
RUN go mod download

# Now copy the rest of the source. Changing main.go no longer busts the
# module-download layer above.
COPY . .

# TARGETOS/TARGETARCH are populated automatically by Docker from the build
# target platform (native platform unless `docker build --platform` is
# passed), so the binary always matches whatever the runtime stage below
# actually needs — instead of a hardcoded amd64 that forces QEMU emulation
# (and Go's netpoller crashes under that) on arm64 hosts like Apple Silicon.
ARG TARGETOS
ARG TARGETARCH

# CGO_ENABLED=0 gives a static binary with no libc dependency, which is what
# lets the final image be `scratch`/distroless instead of a full OS.
# -trimpath and -ldflags="-s -w" drop debug symbols and local file paths,
# shrinking the binary and avoiding leaking build-machine paths into it.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/api .

# ---- runtime stage --------------------------------------------------------
# distroless/static has no shell, no package manager, no libc — nothing an
# attacker can use to pivot even if they get code execution in the container.
# The `nonroot` tag already runs as uid 65532, but we set USER explicitly
# below so the intent survives someone swapping the base image later.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app
COPY --from=build /out/api /app/api

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/app/api"]
