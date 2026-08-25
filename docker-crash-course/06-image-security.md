# 6. Image Security

Four independent controls, each closing a different hole. All four are
already applied in [`app/Dockerfile`](app/Dockerfile),
[`app/.dockerignore`](app/.dockerignore), and [`app/Makefile`](app/Makefile)
— this lesson explains why each one is there.

## 1. Minimal base images: distroless or scratch

A typical `debian`/`ubuntu` base image ships a package manager, a shell, and
hundreds of userland binaries your application never calls — `bash`, `curl`,
`apt`, coreutils. None of that is needed to run a static Go binary, and all
of it is attack surface: every package is something that can carry a CVE
you now have to track and patch, and every shell/binary present is a tool
available to an attacker who gets code execution in your container.

Three tiers, in increasing minimalism:

| Base | Contains | Use when |
|---|---|---|
| `debian-slim` / `alpine` | Package manager, shell, libc | You need to `exec` into the container to debug, or the app needs OS tools at runtime |
| `gcr.io/distroless/static` | Only CA certs + `/etc/passwd` entry — no shell, no package manager, no libc | A statically-linked binary (Go with `CGO_ENABLED=0`) that needs nothing else |
| `scratch` | Literally empty | Same as above, when you don't even need CA certs (no outbound TLS) |

[`app/Dockerfile`](app/Dockerfile) uses
`gcr.io/distroless/static-debian12:nonroot`. Distroless over bare `scratch`
because it still carries an `/etc/passwd` entry for the `nonroot` user and
CA certificates — this API doesn't make outbound TLS calls today, but a
real service usually does eventually (calling another API, an AWS SDK
call), and `scratch` alone has no CA bundle to validate any of that.

The trade-off: no shell means `docker exec -it <container> sh` doesn't
work. Debug distroless containers with `docker debug` (Docker Desktop) or
by attaching an ephemeral debug container to the same pod in Kubernetes
(`kubectl debug`) — not by exec-ing into the minimal one.

## 2. Non-root `USER`

Without an explicit `USER`, a container runs as **root** by default — UID
0. Root inside a container is not as dangerous as root on the host (that's
what [namespaces](01-containers-vs-vms.md) are for), but it's still strictly
more dangerous than it needs to be: a container-breakout vulnerability, a
misconfigured volume mount, or a kernel bug matters far more if the process
that gets out was running as UID 0 than if it was already an unprivileged
user.

```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
...
USER nonroot:nonroot
```

The `:nonroot` image variant already defaults to UID 65532, so this `USER`
line is redundant against the tag today — it's there deliberately, so the
intent survives someone later swapping the base image tag to a plain
`distroless/static-debian12` (which *does* default to root) without
noticing the security property silently disappeared. Verify it directly:

```bash
docker run --rm docker-crash-course-api:dev id 2>/dev/null || \
  docker inspect docker-crash-course-api:dev --format '{{.Config.User}}'
```

(`id` itself won't exist in this image — there's no shell — so
`docker inspect` on the `User` field is the reliable check for a
distroless image.)

## 3. Vulnerability scanning with Trivy

A minimal, non-root image still ships whatever's in its layers — and a
scanner is how you find out if any of that has a known CVE, rather than
assuming.

```bash
# scan a built image directly
trivy image docker-crash-course-api:dev

# what this project's Makefile runs (see app/Makefile)
trivy image --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 \
  docker-crash-course-api:dev
```

What each flag is doing:

- **`--severity HIGH,CRITICAL`** — don't fail the build over every LOW/
  MEDIUM finding; triage those separately instead of blocking every merge.
- **`--ignore-unfixed`** — don't fail on a CVE with no available patch yet.
  There's nothing actionable to do about it today, so failing the build
  only trains people to ignore the scanner.
- **`--exit-code 1`** — makes a qualifying finding fail the process's exit
  code, not just print a report. This is what turns the scan into an
  actual CI gate (see [`ci/github-actions-trivy.yml`](ci/github-actions-trivy.yml))
  instead of output nobody reads.

Run `make scan` in [`app/`](app/) to try it (requires the `trivy` CLI —
`brew install trivy` on macOS). This is also exactly why the multi-stage
build in [lesson 3](03-multistage-builds-go.md) matters for security, not
just size: every package in the discarded `golang:1.24-bookworm` build
stage — the compiler, apt packages, everything — is a package Trivy would
otherwise be scanning and flagging in your *runtime* image, for tooling
that was never present at runtime anyway.

## 4. `.dockerignore`

Covered for caching in [lesson 2](02-dockerfile-anatomy-and-layer-caching.md)
— the security angle is what a *missing* entry lets leak into an image.
`COPY . .` copies everything in the build context that isn't excluded.
Without a `.dockerignore`, that includes `.git/` (full history, potentially
old commits with secrets that were later "removed"), local `.env` files
with real credentials, and any other file matching the standard
`.gitignore`-style patterns you'd never intend to ship.

[`app/.dockerignore`](app/.dockerignore) excludes `.git`, `.env` and
`.env.*`, logs, and local build output — read it alongside the Dockerfile.
Anything landing in a shipped layer is retrievable by anyone who can pull
the image, even from an *earlier* layer overwritten by a later one — union
filesystems (lesson 1) keep every layer, so a secret added in one `COPY`
and deleted in a later `RUN rm` is still sitting in the image history,
extractable with `docker save` + inspecting the layer tarballs. The fix
isn't "delete it in a later layer" — it's never letting it into a layer in
the first place.

## 5. Immutable tags — never `:latest`

`:latest` isn't a version — it's a mutable pointer that gets reassigned to
whatever was pushed most recently. Two problems fall out of that:

- **You can't know what's actually running.** `docker pull myapp:latest`
  today and tomorrow can silently resolve to two different images. A
  rollback (`kubectl rollout undo`, redeploying "the previous version") has
  nothing to roll back *to* if the tag that names "current" has already
  moved on.
- **The build cache and any pinned references lie.** If something else
  cached or pinned `myapp:latest` by digest at pull time, and later someone
  pushes a new image to that same tag, anyone still relying on the tag
  (not the digest) silently gets different bits with no signal that
  anything changed.

Tag by something that uniquely and permanently identifies the build instead
— a git SHA, a semantic version, or both:

```bash
docker build -t docker-crash-course-api:$(git rev-parse --short HEAD) ./app
docker build -t docker-crash-course-api:v1.4.2 ./app
```

[`ci/github-actions-trivy.yml`](ci/github-actions-trivy.yml) computes its
tag from `GITHUB_SHA` for exactly this reason — see
[lesson 7](07-ecr-deployment.md) for how ECR can additionally *enforce*
this at the registry level so a mistagged, mutable push is rejected outright
rather than merely discouraged by convention.

Next: [Shipping to AWS ECR](07-ecr-deployment.md).
