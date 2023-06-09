GOLANGCILINT_VERSION := v1.50

GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
WHITE  := $(shell tput -Txterm setaf 7)
CYAN   := $(shell tput -Txterm setaf 6)
RESET  := $(shell tput -Txterm sgr0)

## Generic

.PHONY: help
help: # show list of all commands
	@awk 'BEGIN {FS = ":.*?# "} { \
		if (/^[a-zA-Z_-]+:.*?#.*$$/) \
			{ printf "  ${YELLOW}%-20s${RESET}${WHTIE}%s${RESET}\n", $$1, $$2} \
		else if (/^## .*$$/) \
			{ printf "${CYAN}%s:${RESET}\n", substr($$1,4)} \
		}' $(MAKEFILE_LIST)


.PHONY: precommit
precommit: # run precommit checks
	gofmt -w -s -d .
	$(MAKE) lint
	$(MAKE) test

.PHONY: install-hook
install-hook: # install git precommit hook
	echo "#!/bin/sh\nmake precommit" > .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit

GOLANGCILINT:=go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCILINT_VERSION)

.PHONY: lint
lint: # lint .go sources
	@$(GOLANGCILINT) run -v --timeout 5m

## Testing

.PHONY: _test-coverprofile
_test-coverprofile:
	@go install golang.org/x/tools/gopls@latest
	@go test ./... -count=1 -cover -race -coverprofile=cover.out

.PHONY: test
test: _test-coverprofile # run tests
	@go tool cover -func=cover.out

.PHONY: test-coverage
test-coverage: _test-coverprofile # run tests and show coverage
	@go tool cover -html cover.out

## Specific to this project

.PHONY: install
install: # install punused cli
	@go install golang.org/x/tools/gopls@latest
	@go install .
