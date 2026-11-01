BINARY      := procure-flow-api
CMD         := ./cmd/api
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
DOCKER_IMAGE ?= procure-flow:local
BASE_URL    ?= http://localhost:8080
COMPOSE     := docker compose -f deployments/docker-compose.yml
# Local env file loaded by the development targets. Already-exported variables
# win over the file; override the path with ENV_FILE=...
ENV_FILE    ?= .env
TEST_DATABASE_URL ?= postgres://procure:procure@127.0.0.1:55432/procure?sslmode=disable
TEST_REDIS_URL ?= redis://127.0.0.1:56379/0
TEST_RABBITMQ_URL ?= amqp://guest:guest@127.0.0.1:5672/

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API locally (loads .env and prints the endpoint)
	@ENV_FILE=$(ENV_FILE) ./scripts/load-env.sh ./scripts/dev-api.sh

.PHONY: worker
worker: ## Run the async worker locally (loads .env; requires RABBITMQ_URL)
	@ENV_FILE=$(ENV_FILE) ./scripts/load-env.sh go run ./cmd/worker

.PHONY: build
build: ## Build the API binary into bin/
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

.PHONY: build-worker
build-worker: ## Build the worker binary into bin/
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY)-worker ./cmd/worker

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: test-race
test-race: ## Run all tests with the race detector
	go test -race ./...

.PHONY: test-integration
test-integration: ## Run all tests against PostgreSQL, Redis, and RabbitMQ
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" \
	TEST_REDIS_URL="$(TEST_REDIS_URL)" \
	TEST_RABBITMQ_URL="$(TEST_RABBITMQ_URL)" \
	go test -race -count=1 ./...

.PHONY: cover
cover: ## Run tests with a coverage summary
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format all Go files
	go fmt ./...

.PHONY: verify-layout
verify-layout: ## Ensure command binaries never link in-memory test doubles
	@if go list -deps ./cmd/... | grep -q 'internal/testsupport'; then \
		echo "error: cmd/ must not depend on internal/testsupport"; exit 1; fi

.PHONY: check
check: fmt vet verify-layout test-integration ## Format, vet, and race-test (with database)

.PHONY: tidy
tidy: ## Tidy modules
	go mod tidy

.PHONY: db-up
db-up: ## Start the local PostgreSQL container
	$(COMPOSE) up -d db
	@echo "PostgreSQL ready on 127.0.0.1:55432 (user/password/db: procure)"

.PHONY: deps-up
deps-up: ## Start PostgreSQL, Redis, and RabbitMQ
	$(COMPOSE) up -d db redis rabbitmq
	@echo "PostgreSQL :55432, Redis :56379, RabbitMQ :5672 (management :15672)"

.PHONY: db-down
db-down: ## Stop the local stack (keeps the volume)
	$(COMPOSE) down

.PHONY: db-reset
db-reset: ## Destroy the local database volume and recreate it
	$(COMPOSE) down -v
	$(COMPOSE) up -d db

.PHONY: migrate-up
migrate-up: ## Apply pending migrations (loads .env)
	@ENV_FILE=$(ENV_FILE) ./scripts/load-env.sh go run ./cmd/migrate up

.PHONY: migrate-status
migrate-status: ## Show migration status (loads .env)
	@ENV_FILE=$(ENV_FILE) ./scripts/load-env.sh go run ./cmd/migrate status

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration (loads .env)
	@ENV_FILE=$(ENV_FILE) ./scripts/load-env.sh go run ./cmd/migrate down

.PHONY: seed
seed: ## Seed demo login accounts (loads .env; SEED_PASSWORD=...)
	@ENV_FILE=$(ENV_FILE) ./scripts/load-env.sh go run ./cmd/seed

.PHONY: jwt-secret
jwt-secret: ## Generate AUTH_JWT_SECRET and write it to .env
	@ENV_FILE=$(ENV_FILE) ./scripts/gen-jwt-secret.sh

.PHONY: docker-build
docker-build: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE) .

.PHONY: smoke
smoke: ## Smoke-test a running instance (BASE_URL=...)
	./scripts/smoke.sh $(BASE_URL)

.PHONY: docs
docs: ## Print the interactive API docs URL
	@echo "API reference: $(BASE_URL)/docs"
	@echo "OpenAPI spec:  $(BASE_URL)/openapi.yaml"

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out
