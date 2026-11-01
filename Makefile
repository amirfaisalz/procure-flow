BINARY      := procure-flow-api
CMD         := ./cmd/api
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
DOCKER_IMAGE ?= procure-flow:local
BASE_URL    ?= http://localhost:8080
COMPOSE     := docker compose -f deployments/docker-compose.yml
TEST_DATABASE_URL ?= postgres://procure:procure@127.0.0.1:55432/procure?sslmode=disable
TEST_REDIS_URL ?= redis://127.0.0.1:56379/0
TEST_RABBITMQ_URL ?= amqp://guest:guest@127.0.0.1:5672/

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API locally
	go run $(CMD)

.PHONY: worker
worker: ## Run the async worker locally (requires RABBITMQ_URL)
	go run ./cmd/worker

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

.PHONY: check
check: fmt vet test-integration ## Format, vet, and race-test (with database)

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
migrate-up: ## Apply pending migrations (DATABASE_URL=...)
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required" && exit 1)
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate up

.PHONY: migrate-status
migrate-status: ## Show migration status (DATABASE_URL=...)
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required" && exit 1)
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate status

.PHONY: seed
seed: ## Seed demo login accounts (DATABASE_URL=...; SEED_PASSWORD=...)
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required" && exit 1)
	DATABASE_URL="$(DATABASE_URL)" SEED_PASSWORD="$(SEED_PASSWORD)" APP_ENV="$(APP_ENV)" go run ./cmd/seed

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
