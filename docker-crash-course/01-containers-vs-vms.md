# 1. Containers vs VMs

## The core difference

A **VM** virtualizes hardware. A hypervisor (KVM, Hyper-V, VMware) presents
each guest with virtual CPU, RAM, disk, and NIC devices; each guest boots its
own kernel and full OS on top of that virtual hardware. Two VMs on one host
are two independent kernels.

A **container** virtualizes the *operating system*, not the hardware. All
containers on a host share the same running kernel. What makes a container
look like an isolated machine is three Linux kernel features working
together:

- **Namespaces** — isolate what a process can *see*
- **cgroups** — limit what a process can *use*
- **Union filesystems** — give each container its own writable filesystem
  view built from shared, read-only layers

There's no second kernel, no virtual BIOS, no boot sequence — a container is
just a regular Linux process with a restricted view of the system, started
in milliseconds instead of the tens of seconds a VM boot takes.

```
VM                                Container
┌─────────────────────────┐       ┌─────────────────────────┐
│ App                      │       │ App                      │
│ Bins/Libs                │       │ Bins/Libs                │
│ Guest OS + Kernel        │       │ (namespaces + cgroups)   │
├─────────────────────────┤       ├─────────────────────────┤
│ Hypervisor                │       │ Container runtime         │
├─────────────────────────┤       ├─────────────────────────┤
│ Host OS + Kernel          │       │ Host OS + Kernel          │
├─────────────────────────┤       ├─────────────────────────┤
│ Hardware                  │       │ Hardware                  │
└─────────────────────────┘       └─────────────────────────┘
```

## Namespaces: what a process can see

Each namespace type hides a category of global system state and gives the
processes inside it their own private copy. A process in a namespace can't
see, and generally can't affect, anything outside it.

| Namespace | Isolates |
|---|---|
| `pid` | Process IDs — container's PID 1 is a normal PID on the host |
| `net` | Network interfaces, routing tables, ports — a container's `eth0` is a veth pair, not the host's NIC |
| `mnt` | Mount points — the container's filesystem root differs from the host's |
| `uts` | Hostname and domain name |
| `ipc` | System V IPC, POSIX message queues |
| `user` | UID/GID mapping — root *inside* the container can map to an unprivileged UID *outside* it |
| `cgroup` | Visibility of the cgroup hierarchy itself |

You can watch this directly:

```bash
docker run --rm -it alpine sh -c 'echo $$; ps aux'
```

Inside the container the shell is PID 1. `ps` shows only that process tree —
none of the host's other processes exist from this namespace's point of
view, even though the kernel scheduling them is the same kernel.

## cgroups: what a process can use

Namespaces control visibility; **control groups (cgroups)** control
resource *consumption* — CPU shares, memory ceilings, block I/O bandwidth,
PID counts. The kernel groups processes into a cgroup and enforces limits on
that group as a whole.

```bash
docker run --rm -it \
  --memory=256m --cpus=0.5 \
  alpine sh
```

This container's cgroup caps it at 256 MiB of memory and half a CPU core.
Exceed the memory limit and the kernel's OOM killer terminates a process in
the cgroup — it can't touch memory reserved for other containers or the
host, and it can't starve its neighbors of CPU either. This is what makes
container resource limits real enforcement, not documentation.

## Union filesystems: layered, shared images

A container image is a stack of read-only layers plus one thin writable
layer on top, merged into a single view by a union filesystem (`overlay2`
is Docker's default on Linux).

```
┌───────────────────────────┐
│ Container writable layer   │  ← changes at runtime live here, and only here
├───────────────────────────┤
│ Layer: COPY . .             │  ← image layers, read-only
├───────────────────────────┤
│ Layer: RUN go mod download  │
├───────────────────────────┤
│ Layer: FROM golang:1.24     │
└───────────────────────────┘
```

Two consequences fall directly out of this design, and both matter for
[layer caching](02-dockerfile-anatomy-and-layer-caching.md):

1. **Layers are content-addressed and shared.** Ten images all built
   `FROM golang:1.24` don't each store their own copy of that base — the
   underlying blobs are shared on disk and reused across every image and
   container that references them.
2. **The writable layer is copy-on-write.** Modifying a file that exists in
   a lower read-only layer copies it up into the writable layer first, then
   modifies the copy. Delete the container and that writable layer — and
   everything written to it — is gone. This is exactly why stateful
   services like Postgres need an explicit **volume** mounted outside the
   union filesystem (see [lesson 5](05-docker-compose-stack.md)) instead of
   relying on the container's own writable layer to survive.

## Why this matters in practice

- **Startup time**: a container is a `fork`/`exec` with some namespace and
  cgroup setup — milliseconds. A VM boots a kernel — seconds to tens of
  seconds.
- **Density**: no per-guest kernel and OS memory tax means you can run far
  more containers than VMs on the same hardware.
- **Isolation strength**: a VM's isolation boundary is the hypervisor,
  enforcing hardware-level separation between guest kernels — a much
  stronger boundary than containers, which share one kernel and rely on
  namespaces/cgroups/capabilities to separate tenants. This is *why*
  [lesson 6](06-image-security.md) cares so much about what runs as root
  inside a container and what the base image even contains: a kernel
  exploit inside a container is a much more direct path to the host than an
  equivalent bug in a VM guest.
- **Portability**: an image bundles the application with exactly the
  userland it needs (or, with distroless, almost none of it) — not a
  virtual machine's worth of OS.

Next: [Dockerfile anatomy and layer caching](02-dockerfile-anatomy-and-layer-caching.md).
