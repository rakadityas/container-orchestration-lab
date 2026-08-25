# 7. How the Image Actually Reaches AWS ECR

## The big idea, in simple words

Until now, every image you built stayed on your own computer. This lesson is
about sending it to a real storage place in the cloud, so servers can
download and run it.

That storage place is called a **registry**. AWS calls its registry **ECR**
(Elastic Container Registry). Docker Hub is another registry you already
used when you pulled `postgres:16-alpine`.

Three things matter here:

1. **Getting in** — you receive a temporary visitor badge, not a permanent
   key.
2. **Locking the labels** — once a box has a name, nobody can move that name
   to a different box.
3. **Cleaning up** — old boxes are deleted automatically, so you do not pay
   forever.

This lesson is reference material. You do not need to run it locally, but
the commands are real and safe to use with a test AWS account.

## Getting in: short-lived tokens, not a stored password

Many registries use a username and password that never expire. ECR does not
work that way, and this is safer.

Instead, the AWS CLI takes your existing AWS identity and exchanges it for a
**temporary token** that is valid for **12 hours**. Then it passes that
token straight to `docker login`.

```bash
aws ecr get-login-password --region us-east-1 \
  | docker login --username AWS --password-stdin \
      <account-id>.dkr.ecr.us-east-1.amazonaws.com
```

Think of it as a **visitor badge at a company reception**. The badge works
today and stops working tomorrow. It is not a copy of the building's master
key.

The real permission comes from **IAM** (AWS's permission system), not from
the token. The token is only a short-lived proof of an IAM identity.

To push an image, the IAM identity needs at minimum:

| Permission | Scope |
|---|---|
| `ecr:GetAuthorizationToken` | The whole registry |
| `ecr:BatchCheckLayerAvailability` | The specific repository |
| `ecr:InitiateLayerUpload` | The specific repository |
| `ecr:UploadLayerPart` | The specific repository |
| `ecr:CompleteLayerUpload` | The specific repository |
| `ecr:PutImage` | The specific repository |

In CI, do **not** store a permanent AWS access key as a secret. Use OIDC
federation instead. With GitHub Actions, the action
`aws-actions/configure-aws-credentials` together with `id-token: write` lets
the job borrow an IAM role temporarily. If there is no permanent credential
stored anywhere, there is no permanent credential to leak.

## Tag, then push

```bash
docker build -t docker-crash-course-api:$(git rev-parse --short HEAD) ./app

docker tag docker-crash-course-api:$(git rev-parse --short HEAD) \
  <account-id>.dkr.ecr.us-east-1.amazonaws.com/docker-crash-course-api:$(git rev-parse --short HEAD)

docker push \
  <account-id>.dkr.ecr.us-east-1.amazonaws.com/docker-crash-course-api:$(git rev-parse --short HEAD)
```

Two things are worth understanding here.

**`docker tag` does not copy anything.** It only adds a second name that
points to the same image. It is like giving a person a nickname — there is
still only one person. This is instant and uses no extra disk space.

**The registry address is part of the name.** The long prefix
`<account-id>.dkr.ecr.us-east-1.amazonaws.com/` is what tells `docker push`
where to send the image. There is no separate "upload" command; the
destination is written inside the tag itself.

## Tag immutability — enforced by the registry

[Lesson 6](06-image-security.md) explained *why* to avoid `:latest` and use
a commit ID instead. But that is only a convention, and a person in a hurry
can still break it.

ECR can enforce the rule for you:

```bash
aws ecr put-image-tag-mutability \
  --repository-name docker-crash-course-api \
  --image-tag-mutability IMMUTABLE
```

After this setting, pushing to a tag that **already exists** is **rejected**
by ECR. It is not overwritten.

Think of it as writing the label in **permanent ink**. Nobody can change it
later — not you, not a colleague, and not a CI job that has valid
credentials.

This gives you a real guarantee: `docker-crash-course-api:a1b2c3d` contains
the same code today and one year from now.

It also turns silent mistakes into loud ones. If a CI job accidentally runs
twice and tries to reuse the same tag, the push fails immediately with a
clear error. Without this setting, it would quietly replace an image that
production may be using.

## Lifecycle policy — because storing everything forever costs money

Every merge creates a new image with a new permanent tag. Permanent tags are
never overwritten and never deleted on their own.

Over time this means two problems: your storage bill grows without limit,
and the ECR console becomes so full that finding anything is difficult.

A **lifecycle policy** is a set of rules, written in JSON, that ECR checks on
its own schedule. It deletes old images for you automatically.

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

The two rules do different jobs:

**Rule 1 — untagged images.** These are almost always leftovers: an old
image that lost its name when a tag moved, or a file left behind by another
process. Nothing points to them by name, so deleting them after 3 days is
safe and aggressive.

**Rule 2 — tagged images beyond a limit.** These are real builds that were
possibly deployed at some time. This rule keeps the newest 30 and deletes
older ones.

Choose the number carefully. The danger is deleting the exact version you
need to roll back to. Pick a number comfortably larger than how far back you
would ever realistically go.

`tagPrefixList` limits the rule to your actual release tags (`v...` and
`main-...`). Without it, the rule could start counting and deleting
unrelated tags stored in the same repository.

## Putting it all together

The pipeline in [`ci/github-actions-trivy.yml`](ci/github-actions-trivy.yml)
continues directly into this lesson. The full chain looks like this:

```
compute an immutable tag from the git commit
        ↓
build the image
        ↓
scan with Trivy — stop here if HIGH or CRITICAL is found
        ↓
log in to ECR using a temporary OIDC role
        ↓
push to a repository where tags cannot be changed
        ↓
lifecycle policy deletes old images automatically
```

Notice what this chain never does: it never trusts a name that can move, and
it never uses a credential that lasts forever.
