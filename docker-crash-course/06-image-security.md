# 6. Image Security

## The big idea, in simple words

Imagine you are shipping a box to a customer.

1. **Put fewer things in the box.** Every extra tool inside is one more thing
   a thief could use. (Minimal base image)
2. **Do not include the master key.** The person opening the box should not
   have permission to do everything. (Non-root user)
3. **Check the box before sending it.** Someone should look for known
   dangerous items. (Vulnerability scanning)
4. **Do not accidentally pack your passport.** Private files must never go
   in. (`.dockerignore`)
5. **Write a permanent serial number on the box.** Not a sticky note that
   anyone can move. (Immutable tags)

These five controls are already applied in [`app/Dockerfile`](app/Dockerfile),
[`app/.dockerignore`](app/.dockerignore), and [`app/Makefile`](app/Makefile).
This lesson explains why each one is there.

## 1. Minimal base images: distroless or scratch

A normal `debian` or `ubuntu` base image contains a package manager, a
shell, and hundreds of programs your application never calls — `bash`,
`curl`, `apt`, and many small tools.

None of that is needed to run a static Go program. And all of it is risk,
for two reasons:

- Every extra package can have a **CVE** (a publicly known security bug)
  that you must now track and fix.
- Every extra program is a **ready-made tool for an attacker** who manages
  to run code inside your container. With no shell and no `curl`, an
  attacker has much less to work with.

Think of it as a house. Fewer doors and windows means fewer ways to break
in.

Three levels, from biggest to smallest:

| Base | What it contains | Use it when |
|---|---|---|
| `debian-slim` / `alpine` | Package manager, shell, C library | You need to open a shell inside the container to debug, or your app calls system tools |
| `gcr.io/distroless/static` | Only CA certificates and a user entry. No shell, no package manager, no C library | Your program is fully static (Go with `CGO_ENABLED=0`) |
| `scratch` | Completely empty | Same as above, and you never make HTTPS calls to the outside |

[`app/Dockerfile`](app/Dockerfile) uses
`gcr.io/distroless/static-debian12:nonroot`.

Why distroless instead of the smaller `scratch`? Because distroless still
includes two useful things: a user entry for the `nonroot` user, and **CA
certificates**. CA certificates are the list of authorities your program
trusts when it makes an HTTPS connection. This API does not make outgoing
HTTPS calls today, but almost every real service eventually does (calling
another API, using an AWS SDK). With `scratch`, those calls would fail
because there is no list to check against.

**The trade-off:** no shell means `docker exec -it <container> sh` does not
work. You cannot open a terminal inside the container. To debug, use
`docker debug` (Docker Desktop), or in Kubernetes attach a temporary debug
container with `kubectl debug`.

## 2. Run as a non-root user

If you do not write a `USER` line, the container runs as **root** (user ID
0), which can do anything.

Root inside a container is less dangerous than root on the host — that is
what [namespaces](01-containers-vs-vms.md) are for. But it is still more
dangerous than necessary. If an attacker finds a way to escape the container,
or if a volume is mounted incorrectly, the damage is much bigger when the
escaping process was root.

```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
...
USER nonroot:nonroot
```

The `:nonroot` version of the image already uses user ID 65532. So this
`USER` line changes nothing today. It is written on purpose anyway.

Here is the reason: if someone later changes the base image to plain
`distroless/static-debian12` (without `:nonroot`), that version **runs as
root**. Without the explicit `USER` line, the security property would
disappear silently and nobody would notice. The line keeps the intention
visible.

Check which user the image uses:

```bash
docker inspect docker-crash-course-api:dev --format '{{.Config.User}}'
```

Do not try `docker run ... id` here. The `id` program does not exist in a
distroless image, because there is no shell and no system tools.

## 3. Scan for vulnerabilities with Trivy

A small image running as a non-root user can still contain a package with a
known security bug. A **scanner** tells you if that is true, instead of you
guessing.

Trivy compares everything inside your image against public CVE databases.

```bash
# scan an image
trivy image docker-crash-course-api:dev

# what this project's Makefile runs
trivy image --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 \
  docker-crash-course-api:dev
```

What each flag does:

- **`--severity HIGH,CRITICAL`** — only fail on serious problems. If you
  fail the build on every LOW and MEDIUM finding, developers will start
  ignoring the scanner completely.
- **`--ignore-unfixed`** — ignore problems that have **no fix available
  yet**. There is nothing you can do about them today, so stopping the build
  only creates frustration.
- **`--exit-code 1`** — make the command **fail** when it finds something,
  instead of only printing a report. This is what turns the scan into a real
  gate in CI (see [`ci/github-actions-trivy.yml`](ci/github-actions-trivy.yml)).
  A report nobody reads stops nothing.

Try it with `make scan` inside [`app/`](app/). You need the Trivy CLI first
(`brew install trivy` on macOS).

This also shows why the multi-stage build from
[lesson 3](03-multistage-builds-go.md) helps **security**, not only size.
Every package in the discarded `golang:1.24-bookworm` build stage — the
compiler, all the Debian packages — would otherwise be scanned and reported
in your runtime image, for tools that were never needed at runtime.

> **Note:** your editor may warn that `golang:1.24-bookworm` has many
> vulnerabilities. That warning is about the **build stage**, which is
> thrown away and never shipped. It is not your runtime image.

## 4. `.dockerignore`

[Lesson 2](02-dockerfile-anatomy-and-layer-caching.md) covered how this file
protects your cache. Here is the security side.

`COPY . .` copies **everything** in the build context that is not excluded.
Without a `.dockerignore`, that can include:

- `.git/` — your full history, including old commits that contained
  passwords which were "removed" later
- `.env` files with real credentials
- log files and local build output

[`app/.dockerignore`](app/.dockerignore) excludes exactly these.

### Why deleting a secret later does not help

This part surprises many people.

Remember from [lesson 1](01-containers-vs-vms.md) that an image is a stack
of transparent sheets, and **every sheet is kept**. If you add a secret in
one `COPY` line and delete it in a later `RUN rm` line, the file is only
*covered*, not removed. It still sits in the earlier layer.

Anyone who can pull your image can extract it with `docker save` and read
the layer files directly.

> The fix is never "delete it in a later step". The fix is **never letting it
> into any layer in the first place.**

If a secret ever does reach a published image, treat it as leaked and rotate
it. Do not simply build a new image.

## 5. Immutable tags — never use `:latest`

`:latest` is not a version. It is just a name that points to whatever was
pushed most recently. Anyone can move it.

Think of `:latest` as a **sticky note** on a shelf that says "newest". Every
time a new box arrives, someone moves the note. If you ask for "the box with
the note", you do not know which box you will get.

Two real problems come from this:

- **You cannot know what is running.** `docker pull myapp:latest` today and
  tomorrow can give you two completely different images. And if you need to
  roll back to the previous version, there is no name that still points to
  it. It is gone.
- **Pinned references become lies.** If a system pulled `myapp:latest`
  yesterday and someone pushes a new image to that same tag today, anyone
  using the tag now silently gets different code, with no warning that
  anything changed.

Instead, tag with something that is unique and permanent, like a git commit
ID or a version number:

```bash
docker build -t docker-crash-course-api:$(git rev-parse --short HEAD) ./app
docker build -t docker-crash-course-api:v1.4.2 ./app
```

[`ci/github-actions-trivy.yml`](ci/github-actions-trivy.yml) builds its tag
from `GITHUB_SHA` for exactly this reason.

Tagging by commit ID is still only a habit, and habits can be broken by
mistake. [Lesson 7](07-ecr-deployment.md) shows how ECR can **enforce** this
rule at the registry, so a wrong push is rejected instead of accepted.

Next: [Shipping to AWS ECR](07-ecr-deployment.md).
