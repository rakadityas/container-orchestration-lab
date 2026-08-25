IMAGE ?= docker-crash-course-api
TAG   ?= dev

.PHONY: build scan run

build:
	docker build -t $(IMAGE):$(TAG) .

# Fails the build (exit code 1) on any HIGH or CRITICAL vulnerability with a
# known fix available. See ../06-image-security.md for what each flag does.
scan: build
	trivy image \
		--severity HIGH,CRITICAL \
		--ignore-unfixed \
		--exit-code 1 \
		$(IMAGE):$(TAG)

run: build
	docker run --rm -p 8080:8080 \
		-e DATABASE_URL=postgres://postgres:postgres@host.docker.internal:5432/appdb?sslmode=disable \
		-e REDIS_ADDR=host.docker.internal:6379 \
		$(IMAGE):$(TAG)
