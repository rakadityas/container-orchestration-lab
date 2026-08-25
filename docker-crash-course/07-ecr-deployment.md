# 7. How the Image Actually Reaches AWS ECR

Everything so far produces a local image. This lesson covers the last mile:
authenticating to a registry, pushing, and the two settings —
tag immutability and lifecycle policy — that keep an ECR repository from
becoming a liability once it's live. Reference only; no local execution
needed, but the commands are real and safe to run against a sandbox AWS
account if you have one.

## Registry auth: short-lived tokens, not a stored password

ECR doesn't use a long-lived username/password like some registries. The
AWS CLI exchanges your existing AWS credentials (an IAM user, role, or
instance/task role) for a registry auth token that's valid for **12
hours**, and pipes it straight into `docker login`:

```bash
aws ecr get-login-password --region us-east-1 \
  | docker login --username AWS --password-stdin \
      <account-id>.dkr.ecr.us-east-1.amazonaws.com
```

What's actually granting access here is IAM, not that token — the token is
just a short-lived credential *derived from* whatever IAM identity ran the
command. The IAM principal needs `ecr:GetAuthorizationToken` (registry-wide)
plus `ecr:BatchCheckLayerAvailability`, `ecr:PutImage`,
`ecr:InitiateLayerUpload`, `ecr:UploadLayerPart`, and
`ecr:CompleteLayerUpload` scoped to the specific repository, at minimum, to
push. In CI, this is normally OIDC federation (GitHub Actions' `aws-actions/
configure-aws-credentials` assuming a role via `id-token: write`) rather
than a long-lived IAM user access key sitting in a CI secret — no
long-lived AWS credential to leak in the first place.

## Tag, then push

```bash
docker build -t docker-crash-course-api:$(git rev-parse --short HEAD) ./app

docker tag docker-crash-course-api:$(git rev-parse --short HEAD) \
  <account-id>.dkr.ecr.us-east-1.amazonaws.com/docker-crash-course-api:$(git rev-parse --short HEAD)

docker push \
  <account-id>.dkr.ecr.us-east-1.amazonaws.com/docker-crash-course-api:$(git rev-parse --short HEAD)
```

`docker tag` doesn't copy anything — it adds a second name pointing at the
same image ID. The registry hostname baked into the tag
(`<account-id>.dkr.ecr.<region>.amazonaws.com/...`) is what tells `docker
push` where to send it; ECR has no separate "upload" step distinct from
this.

## Tag immutability — enforced at the registry, not just by convention

[Lesson 6](06-image-security.md) covered *why* to avoid `:latest` and tag
by SHA/version instead — that's a convention anyone can still break by
typo or habit. ECR can enforce it as a repository setting:

```bash
aws ecr put-image-tag-mutability \
  --repository-name docker-crash-course-api \
  --image-tag-mutability IMMUTABLE
```

With this set, `docker push` to a tag that **already exists** in the
repository is rejected outright by ECR — not overwritten. This is what
actually guarantees `docker-crash-course-api:a1b2c3d` means the same bits
today and a year from now: nobody, including a CI job with valid push
credentials, can silently replace what that tag points to. It also forces a
real mistake to surface immediately (a re-run that tries to reuse a SHA
tag fails loudly) instead of quietly overwriting a production reference.

## Lifecycle policy — because "keep everything forever" isn't free

Every merge that triggers a build produces a new immutable tag; immutable
tags are never overwritten and never automatically deleted. Left alone,
that's unbounded storage growth (billed) and an ECR console impossible to
find anything useful in. A **lifecycle policy** is a JSON ruleset ECR
evaluates on its own schedule to expire images automatically:

```json
{
  "rules": [
    {
      "rulePriority": 1,
      "description": "Expire untagged images after 3 days",
      "selection": {
        "tagStatus": "untagged",
        "countType": "sinceImagePushed",
        "countUnit": "days",
        "countNumber": 3
      },
      "action": { "type": "expire" }
    },
    {
      "rulePriority": 2,
      "description": "Keep only the most recent 30 tagged images",
      "selection": {
        "tagStatus": "tagged",
        "tagPrefixList": ["v", "main-"],
        "countType": "imageCountMoreThan",
        "countNumber": 30
      },
      "action": { "type": "expire" }
    }
  ]
}
```

```bash
aws ecr put-lifecycle-policy \
  --repository-name docker-crash-course-api \
  --lifecycle-policy-text file://lifecycle-policy.json
```

Two rule shapes worth distinguishing:

- **Untagged images** (`tagStatus: untagged`) — these are almost always
  cache byproducts: an old layer superseded when a tag was reassigned
  during a build's intermediate stages, or a manifest orphaned by some
  other process. Rule 1 clears these aggressively (3 days) since nothing
  should be referencing them by tag.
- **Tagged images past a retention count** (`tagStatus: tagged` +
  `imageCountMoreThan`) — real, immutable, previously-deployed builds.
  Rule 2 keeps the most recent 30 and expires older ones. The count needs
  to comfortably exceed how far back you'd ever plausibly roll back to —
  losing the one tag currently referenced by a rollback target is the
  failure mode to design around. `tagPrefixList` scopes this to your actual
  release tags so it doesn't also start counting and expiring unrelated
  tag patterns in the same repository.

## Putting it together

The pipeline from [`ci/github-actions-trivy.yml`](ci/github-actions-trivy.yml)
extends directly into this lesson: compute an immutable tag → build → scan
with Trivy and fail on HIGH/CRITICAL → authenticate to ECR via OIDC-assumed
IAM role → push to a tag-immutable repository → let the lifecycle policy
handle cleanup on its own schedule. Nothing in that chain trusts a mutable
pointer or a long-lived credential at any step.
