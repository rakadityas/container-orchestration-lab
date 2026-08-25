# 1. Containers vs VMs

## The big idea, in simple words

Imagine a big apartment building.

- A **virtual machine (VM)** is like building a **whole new house** next to
  it. The new house has its own foundation, its own water pipes, and its own
  electricity. It is very separate, but it is slow and expensive to build.
- A **container** is like **renting one room** inside the apartment building
  that already exists. You share the water pipes and the electricity with
  everyone else. But you have your own door, your own lock, and your own
  furniture. It is fast and cheap to move in.

The "water pipes and electricity" in this story is the **kernel** — the core
part of the operating system that talks to the hardware.

## The technical difference

A **VM** copies the *hardware*. A program called a hypervisor (KVM, Hyper-V,
VMware) gives each guest a fake CPU, fake RAM, fake disk, and fake network
card. Each guest then starts its own kernel and its own full operating
system. Two VMs on one machine means two separate kernels.

A **container** copies the *operating system*, not the hardware. Every
container on one machine shares the same kernel. A container only *looks*
like a separate machine. Three features of the Linux kernel create this
illusion:

- **Namespaces** — control what a program can *see*
- **cgroups** — control how much a program can *use*
- **Union filesystems** — give each container its own files, built from
  shared, read-only pieces

There is no second kernel and no boot sequence. A container is just a normal
Linux program with a limited view of the system. It starts in milliseconds.
A VM needs seconds, sometimes a minute, because it must boot a full
operating system first.

```
VM                                Container
┌──────────────────────────┐      ┌──────────────────────────┐
│ App                      │      │ App                      │
│ Bins/Libs                │      │ Bins/Libs                │
│ Guest OS + Kernel        │      │ (namespaces + cgroups)   │
├──────────────────────────┤      ├──────────────────────────┤
│ Hypervisor               │      │ Container runtime        │
├──────────────────────────┤      ├──────────────────────────┤
│ Host OS + Kernel         │      │ Host OS + Kernel         │
├──────────────────────────┤      ├──────────────────────────┤
│ Hardware                 │      │ Hardware                 │
└──────────────────────────┘      └──────────────────────────┘
```

Look at the difference: the VM box has an extra "Guest OS + Kernel" layer.
The container box does not. That missing layer is the whole reason
containers are small and fast.

## Namespaces: what a program can see

Think of a namespace like a **wall with no windows**. The program inside
cannot see what is outside. It does not even know the outside exists.

Each type of namespace hides one category of system information and gives
the program its own private copy.

| Namespace | What it hides |
|---|---|
| `pid` | Process IDs — the container thinks it has PID 1, but the host sees a normal, different number |
| `net` | Network cards, routing tables, ports — the container's `eth0` is a virtual cable, not the real network card |
| `mnt` | Mount points — the container sees a different set of folders than the host |
| `uts` | Hostname and domain name |
| `ipc` | Shared memory and message queues between programs |
| `user` | User IDs — "root" *inside* the container can be a normal, weak user *outside* it |
| `cgroup` | Whether the program can see the cgroup structure itself |

You can see this yourself:

```bash
docker run --rm -it alpine sh -c 'echo $$; ps aux'
```

Inside the container, the shell is PID 1 (the first process). `ps` shows
only that one small group of processes. All the other programs on your
computer are invisible, even though the same kernel is running all of them.

## cgroups: how much a program can use

Namespaces decide what you can **see**. cgroups (short for "control groups")
decide how much you can **take** — CPU, memory, disk speed, number of
processes.

Think of cgroups like a **water meter with a limit**. You can open the tap,
but only so much water comes out.

```bash
docker run --rm -it \
  --memory=256m --cpus=0.5 \
  alpine sh
```

This container can use at most 256 MiB of memory and half of one CPU core.
If it tries to use more memory, the kernel kills a process inside it. It
cannot steal memory from other containers, and it cannot slow them down by
taking all the CPU.

This is real enforcement by the kernel. It is not a suggestion or a comment
in a document.

## Union filesystems: layers that are shared

A container image is a **stack of read-only layers**, plus one thin
writable layer on top. A union filesystem (Docker uses `overlay2` on Linux)
merges them so they look like one normal set of folders.

Think of it like a **stack of transparent sheets**. Each sheet has some
drawings on it. When you look down through the whole stack, you see one
combined picture. Only the top sheet can be drawn on.

```
┌────────────────────────────┐
│ Container writable layer   │  ← runtime changes go here, and only here
├────────────────────────────┤
│ Layer: COPY . .            │  ← image layers, read-only
├────────────────────────────┤
│ Layer: RUN go mod download │
├────────────────────────────┤
│ Layer: FROM golang:1.24    │
└────────────────────────────┘
```

Two important results come from this design. Both matter for
[layer caching](02-dockerfile-anatomy-and-layer-caching.md):

1. **Layers are shared between images.** If ten images all start
   `FROM golang:1.24`, they do not each keep their own copy. The files are
   stored once on disk and reused by every image that needs them. This saves
   a lot of space.
2. **The writable layer is copy-on-write.** If you change a file that lives
   in a lower read-only layer, the system first copies that file up to the
   writable layer, then changes the copy. When you delete the container, the
   writable layer is deleted too — and everything in it is gone forever.

Result number 2 is exactly why a database like Postgres needs a **volume**.
A volume is storage that lives *outside* this layer stack, so it survives
when the container is deleted. See [lesson 5](05-docker-compose-stack.md).

## Why this matters in real work

- **Startup time**: starting a container means starting a process and
  setting up namespaces and cgroups. This takes milliseconds. A VM must boot
  a kernel, which takes seconds or longer.
- **Density**: containers do not each carry their own kernel and operating
  system, so they use much less memory. You can run many more containers
  than VMs on the same machine.
- **Isolation strength**: here VMs win. A VM is separated by the hypervisor,
  which enforces separation at the hardware level. Containers share one
  kernel and rely on namespaces, cgroups, and capabilities instead. That is
  a weaker wall.

  This is *why* [lesson 6](06-image-security.md) cares so much about running
  as a non-root user and using a small base image. If an attacker escapes a
  container, they are already very close to the host. If an attacker escapes
  a VM guest, they still have the hypervisor in their way.
- **Portability**: an image contains your application plus only the files it
  truly needs. With distroless images, that is almost nothing extra. A VM
  image contains a whole operating system.

Next: [Dockerfile anatomy and layer caching](02-dockerfile-anatomy-and-layer-caching.md).
