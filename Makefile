NAME          := openshuttercount
COVERAGE_FILE := $(CURDIR)/coverage.out
REPOSITORY    := DigitalN/Open-Shutter-Count

# One Go version everywhere, the same as tool/mac/build.sh's: the tool's bytes depend
# on it, and golangci-lint can't read what Go 1.27.3 compiles. Any Go 1.21 or
# later fetches this version itself.
export GOTOOLCHAIN := go1.27.0

# The tool's Go module is in tool/, so Go runs there.
GO            := go -C tool

GOLANGCI_LINT := $(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint

# Building the linter once lets it be re-run under GOOS=darwin from any
# machine. `go tool` would cross compile the linter itself instead. It runs in
# tool/, where it finds tool/.golangci.yml.
GOLANGCI_BIN  := .bin/golangci-lint
LINT_BIN      := $(CURDIR)/$(GOLANGCI_BIN)

.DEFAULT_GOAL := help

.PHONY: fmt
fmt: ## Format source code
	$(GOLANGCI_LINT) fmt -v

.PHONY: lint
lint: ## Run all lint related tests upon the codebase
	$(GOLANGCI_LINT) run -v

.PHONY: lint-mac
lint-mac: $(GOLANGCI_BIN) ## Lint the macOS USB backend too, from any machine
	cd tool && GOOS=darwin $(LINT_BIN) run

$(GOLANGCI_BIN):
	@mkdir -p $(dir $@)
	$(GO) build -o $(CURDIR)/$@ github.com/golangci/golangci-lint/v2/cmd/golangci-lint

.PHONY: test
test: ## Run the tests against the codebase
	@rm -rf $(COVERAGE_FILE)
	$(GO) test -v -count=1 -race ./... -coverprofile=$(COVERAGE_FILE)
	@$(GO) tool cover -func $(COVERAGE_FILE) | awk '/^total/ {print "coverage: " $$3}'

.PHONY: coverage
coverage: ## Prints coverage report
	$(GO) tool cover -func $(COVERAGE_FILE)

.PHONY: coverage-html
coverage-html: ## Generates coverage report and displays it in the browser
	$(GO) tool cover -html=$(COVERAGE_FILE)

.PHONY: install
install: ## Build and install locally the binary (dev purpose)
	$(GO) install ./cmd/$(NAME)

.PHONY: build
build: ## Build the binary using local GOOS
	$(GO) build -o $(CURDIR)/$(NAME) ./cmd/$(NAME)

.PHONY: build-mac
build-mac: ## Build the Mac binary, fetch libusb and its source into tool/mac/ (see tool/mac/build.sh)
	bash tool/mac/build.sh

.PHONY: clean
clean: ## Remove build artefacts, keeping the release DMGs in dist/
	rm -rf $(NAME) $(COVERAGE_FILE) .bin helpers

.PHONY: is-git-dirty
is-git-dirty: ## Tests if git is in a dirty state
	@git status --porcelain
	@test $(shell git status --porcelain | grep -c .) -eq 0

.PHONY: all
all: lint lint-mac test coverage ## Lint here and for the Mac, and test

# The Mac binary, libusb and its source, and the app's fixtures, which are made from the
# Go code or fetched against a pinned hash, and committed. All come out byte for byte the
# same every time they're made.
GENERATED := tool/mac/openshuttercount tool/mac/libusb-1.0.dylib tool/mac/libusb-1.0.30.tar.bz2 app/tests/fixtures
GENERATED_SUM = find $(GENERATED) -type f -exec cksum {} + | sort

.PHONY: generated
generated: ## Remake the Mac binary, libusb, its source and the app's fixtures; fail if they were stale
	@command -v go >/dev/null || { echo "make check needs Go: https://go.dev/dl/"; exit 1; }
	@before="$$($(GENERATED_SUM))"; \
	bash tool/mac/build.sh >/dev/null && \
	UPDATE_FIXTURES=1 $(GO) test -count=1 ./internal/cmd -run TestAppFixtures >/dev/null && \
	if [ "$$before" != "$$($(GENERATED_SUM))" ]; then \
		git status --short -- $(GENERATED); \
		echo "These were stale and are remade now: commit them, then run make check again."; \
		exit 1; \
	fi

# Fails if the project's old name is back in a tracked file. See .github/check-name.sh.
.PHONY: check-name
check-name: ## Fail if the project's old name has come back
	@bash .github/check-name.sh

# Everything that has to pass before a push. There's no CI: .github/hooks/pre-push
# runs this, and .claude/hooks/session-start.sh turns that hook on.
.PHONY: check
check: check-name generated $(GOLANGCI_BIN) ## Everything that must pass before a push
	$(GO) vet ./...
	GOOS=darwin $(GO) vet ./...
	$(GO) test -count=1 ./...
	cd tool && $(LINT_BIN) run ./...
	cd tool && GOOS=darwin $(LINT_BIN) run ./...
	python3 -m pytest -q app/tests

.PHONY: help
help: ## Displays this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
