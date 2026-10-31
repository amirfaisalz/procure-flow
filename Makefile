BINARY      := procure-flow-api
CMD         := ./cmd/api
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
DOCKER_IMAGE ?= procure-flow:local
BASE_URL    ?= http://localhost:8080

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API locally
	go run $(CMD)

.PHONY: build
build: ## Build the API binary into bin/
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: test-race
test-race: ## Run all tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run tests with a coverage summary
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format all Go files
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

.PHONY: check
check: fmt vet test-race ## Format, vet, and race-test

.PHONY: tidy
tidy: ## Tidy modules
	go mod tidy

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
