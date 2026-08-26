# 13. Helm: an Introduction

## The big idea, in simple words

By now you have written many YAML files. Imagine you need the same
application in **three environments**: development, staging, and production.
Almost everything is identical. Only a few values differ — replica count,
image tag, resource sizes, hostname.

You have three bad options:

1. Copy the folder three times → three copies to keep in sync, forever.
2. Edit files by hand before each deploy → someone will make a mistake.
3. Use a **template with blanks to fill in** → this is Helm.

Helm is a **form letter** for Kubernetes. You write the letter once with
blanks, then supply a small list of answers for each environment.

```
templates (the letter with blanks)  +  values (the answers)  =  final YAML
```

## The vocabulary

| Word | Meaning |
|---|---|
| **Chart** | A folder of templates — the package |
| **Values** | The answers that fill the blanks |
| **Release** | One installation of a chart, with a name |
| **Repository** | A place charts are published |

You can install the same chart many times with different names and values.
Each installation is a separate release.

## What a chart looks like

```
api-chart/
├── Chart.yaml           # name, version, description
├── values.yaml          # DEFAULT answers
└── templates/
    ├── deployment.yaml
    ├── service.yaml
    ├── ingress.yaml
    └── _helpers.tpl     # reusable snippets
```

`values.yaml` holds the defaults:

```yaml
replicaCount: 3

image:
  repository: docker-crash-course-api
  tag: dev
  pullPolicy: IfNotPresent

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    memory: 256Mi

ingress:
  enabled: true
  host: api.local
```

And a template uses those values, with `{{ }}` marking each blank:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}-api
spec:
  replicas: {{ .Values.replicaCount }}
  template:
    spec:
      containers:
        - name: api
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
```

Two kinds of blank appear here:

- `.Values.something` — comes from `values.yaml` or the command line
- `.Release.Name` — provided by Helm, the name you chose at install time

Templates can also make decisions:

```yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
...
{{- end }}
```

Now `ingress.enabled: false` simply removes the Ingress from the output.

## The commands

```bash
helm install api ./api-chart -n demo              # first install
helm install api ./api-chart -n demo -f prod.yaml # with different answers
helm install api ./api-chart --set replicaCount=5 # override one value

helm upgrade api ./api-chart -n demo              # change an existing release
helm upgrade --install api ./api-chart -n demo    # install OR upgrade (use in CI)

helm list -n demo                                  # what is installed
helm history api -n demo                           # every revision
helm rollback api 2 -n demo                        # go back to revision 2
helm uninstall api -n demo                         # remove everything
```

`helm upgrade --install` is the one to use in automated pipelines. It works
whether or not the release already exists, so the same command is safe every
time.

### See the result before applying it

These two are the most useful commands while learning:

```bash
helm template api ./api-chart              # render locally, apply nothing
helm install api ./api-chart --dry-run --debug
```

`helm template` prints the finished YAML. When a chart behaves strangely,
render it and read what was actually produced. Most confusion disappears
immediately.

## Why Helm is genuinely useful

### 1. One chart, many environments

```bash
helm upgrade --install api ./api-chart -f values-dev.yaml
helm upgrade --install api ./api-chart -f values-prod.yaml
```

```yaml
# values-prod.yaml — only the differences
replicaCount: 10
image:
  tag: v1.4.2
resources:
  requests:
    cpu: 500m
    memory: 512Mi
ingress:
  host: api.example.com
```

Small, readable, and reviewable in a pull request.

### 2. Installing other people's software

This is how most people first meet Helm:

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx -n ingress-nginx --create-namespace
```

That single command replaces the long URL we used in
[lesson 6](06-deploy-to-kind.md), and lets you configure the installation
with values. Most well-known projects (Prometheus, Grafana, cert-manager,
External Secrets Operator) ship official charts.

### 3. Rollback of a whole release

`kubectl rollout undo` reverts **one Deployment**. `helm rollback` reverts
**everything in the release together** — Deployment, ConfigMap, Service,
Ingress — back to a known-good revision.

### 4. The ConfigMap restart trick

Remember the problem from [lesson 4](04-config-and-secrets.md): changing a
ConfigMap does **not** restart pods, so they keep using old values.

Helm solves it neatly:

```yaml
spec:
  template:
    metadata:
      annotations:
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
```

The annotation contains a checksum of the ConfigMap. Change the ConfigMap →
the checksum changes → the pod template changes → Kubernetes performs a
rolling update automatically. This little line is one of the best reasons to
use Helm.

## When Helm is the wrong tool

Helm has real downsides. Be honest about them:

- **Templated YAML gets ugly.** Whitespace control (`{{-`, `nindent`) is
  fiddly, and a complex chart can become unreadable.
- **It is text templating, not structure-aware.** Helm does not understand
  Kubernetes objects; it produces text and hopes it is valid YAML.
- **Debugging is indirect.** You must render first to see what happened.

The main alternative is **Kustomize**, which is built into kubectl:

```bash
kubectl apply -k overlays/production/
```

Kustomize takes plain YAML and applies **patches** on top. No templating
language at all.

| | Helm | Kustomize |
|---|---|---|
| Approach | Templates with blanks | Plain YAML + patches |
| Distributing to others | ✅ Excellent | ⚠️ Harder |
| Readability | ⚠️ Can get messy | ✅ Stays plain YAML |
| Install | Separate tool | Built into `kubectl` |
| Rollback | ✅ Built in | ❌ Use git |

A common practical split:

- **Helm** for installing third-party software.
- **Kustomize** for your own application's manifests.

Both are correct. Neither is required — the plain manifests in this course
work perfectly well for a single environment.

## Try it

Convert the manifests in [`manifests/base/`](manifests/base/) into a chart:

```bash
helm create api-chart          # generates a working example chart
```

Read the generated `templates/deployment.yaml` alongside your own
`05-api.yaml`. You will recognise every field — the only difference is the
blanks.

Next: [Production readiness checklist and quiz](14-production-readiness.md).
