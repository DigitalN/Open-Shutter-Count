NAME          := openshuttercounter
COVERAGE_FILE := coverage.out
REPOSITORY    := DigitalN/$(NAME)

GOLANGCI_LINT := go tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint

# Building the linter once lets it be re-run under GOOS=darwin from any
# machine. `go tool` would cross compile the linter itself instead.
GOLANGCI_BIN  := .bin/golangci-lint

.DEFAULT_GOAL := help

.PHONY: fmt
fmt: ## Format source code
	$(GOLANGCI_LINT) fmt -v

.PHONY: lint
lint: ## Run all lint related tests upon the codebase
	$(GOLANGCI_LINT) run -v

.PHONY: lint-mac
lint-mac: $(GOLANGCI_BIN) ## Lint the macOS USB backend too, from any machine
	GOOS=darwin $(GOLANGCI_BIN) run

$(GOLANGCI_BIN):
	@mkdir -p $(dir $@)
	go build -o $@ github.com/golangci/golangci-lint/v2/cmd/golangci-lint

.PHONY: test
test: ## Run the tests against the codebase
	@rm -rf $(COVERAGE_FILE)
	go test -v -count=1 -race ./... -coverprofile=$(COVERAGE_FILE)
	@go tool cover -func $(COVERAGE_FILE) | awk '/^total/ {print "coverage: " $$3}'

.PHONY: coverage
coverage: ## Prints coverage report
	go tool cover -func $(COVERAGE_FILE)

.PHONY: coverage-html
coverage-html: ## Generates coverage report and displays it in the browser
	go tool cover -html=$(COVERAGE_FILE)

.PHONY: install
install: ## Build and install locally the binary (dev purpose)
	go install ./cmd/$(NAME)

.PHONY: build
build: ## Build the binary using local GOOS
	go build ./cmd/$(NAME)

.PHONY: build-mac
build-mac: ## Build the Mac binary and libusb into mac/ (see mac/build.sh)
	bash mac/build.sh

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf $(NAME) $(COVERAGE_FILE) .bin dist helpers

.PHONY: is-git-dirty
is-git-dirty: ## Tests if git is in a dirty state
	@git status --porcelain
	@test $(shell git status --porcelain | grep -c .) -eq 0

.PHONY: all
all: lint lint-mac test coverage ## Lint here and for the Mac, and test

# The Mac binary and the app's fixtures, which are made from the Go code and
# committed. Both come out byte for byte the same every time they're made.
GENERATED := mac/openshuttercounter mac/libusb-1.0.dylib app/tests/fixtures
GENERATED_SUM = find $(GENERATED) -type f -exec cksum {} + | sort

.PHONY: generated
generated: ## Remake the Mac binary and the app's fixtures; fail if they were stale
	@command -v go >/dev/null || { echo "make check needs Go: https://go.dev/dl/"; exit 1; }
	@before="$$($(GENERATED_SUM))"; \
	bash mac/build.sh >/dev/null && \
	UPDATE_FIXTURES=1 go test -count=1 ./internal/cmd -run TestAppFixtures >/dev/null && \
	if [ "$$before" != "$$($(GENERATED_SUM))" ]; then \
		git status --short -- $(GENERATED); \
		echo "These were stale and are remade now: commit them, then run make check again."; \
		exit 1; \
	fi

# Everything that has to pass before a push. There's no CI: .githooks/pre-push
# runs this, and .claude/hooks/session-start.sh turns that hook on.
.PHONY: check
check: generated $(GOLANGCI_BIN) ## Everything that must pass before a push
	go vet ./...
	GOOS=darwin go vet ./...
	go test -count=1 ./...
	$(GOLANGCI_BIN) run ./...
	GOOS=darwin $(GOLANGCI_BIN) run ./...
	python3 -m pytest -q app/tests

.PHONY: help
help: ## Displays this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
