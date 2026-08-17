# uses only the standard go toolchain; no external linters

.DEFAULT_GOAL := all

.PHONY: all build install test fuzz bench cover cover-html fmt fmt-check vet lint clean help

all: lint test build ## run lint, tests, and build

build: ## build all packages
	go build ./...

install: ## install the muid binary into ~/.local/bin
	@mkdir -p $(HOME)/.local/bin
	GOBIN=$(HOME)/.local/bin go install ./cmd/muid

test: ## run the race-enabled test suite
	go test -race ./...

fuzz: ## run the parse fuzz target
	go test -run=NONE -fuzz=FuzzParse -fuzztime=10s .

bench: ## run benchmarks
	go test -bench=. -benchtime=100x -run=NONE .

cover: ## generate and summarize the coverage profile
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

cover-html: cover ## generate an HTML coverage report
	go tool cover -html=coverage.out -o coverage.html

fmt: ## format Go source files
	gofmt -w .

fmt-check: ## check Go source formatting
	@test -z "$$(gofmt -l .)" || { echo "files needing formatting:"; gofmt -l .; exit 1; }

vet: ## run go vet
	go vet ./...

lint: fmt-check vet ## check formatting and run go vet

clean: ## remove build and coverage artifacts
	go clean
	rm -f muid coverage.out coverage.html

help: ## list available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9][^:]*:.*## / {printf "%-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
