.PHONY: build test lint clean install verify vuln doc-coverage hooks

BINARY=vxd
VERSION?=0.1.0
LDFLAGS=-ldflags "-X main.version=$(VERSION)"
INSTALL_DIR?=$(shell go env GOPATH)/bin

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/vxd/

test:
	go test ./... -race -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

# doc-coverage runs only the documentation-enforcement wiring tests
# (TestDocCoverage_*). Fast pre-push gate: catches an undocumented CLI command
# or config field before it reaches a PR.
doc-coverage:
	go test ./internal/engine/ -run TestDocCoverage -count=1

# vuln scans the module + its dependencies for known CVEs. Non-fatal advisories
# still exit non-zero, so this is also the weekly-scheduled security gate (G).
vuln:
	govulncheck ./...

# verify is the single source of truth for the green gate (conditions C + D).
# The pre-push hook and the audit loop both call it. Ordered cheapest-first so
# a fast failure (vet) stops before the slow race-tested suite runs.
verify:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/vxd/
	go vet ./...
	golangci-lint run ./...
	go test ./... -count=1
	$(MAKE) doc-coverage

clean:
	rm -f $(BINARY) coverage.out

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/

# hooks wires the repo-tracked .githooks/ as git's hook path so the green-gate
# pre-push hook is shared, not a per-clone manual step. Run once after cloning.
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks installed (core.hooksPath=.githooks)"
