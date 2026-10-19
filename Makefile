GO ?= go
NODE ?= node
NPM ?= npm
export GO
export PATH := $(abspath .tools/bin):$(dir $(GO)):$(dir $(NODE)):$(PATH)

.PHONY: build test check check-go check-ui tools check-lint check-diagnostics check-format

build:
	$(GO) build -buildvcs=false -trimpath -o wpp ./cmd/wpp

test:
	$(GO) test -race ./...

check: check-security check-format check-lint check-diagnostics check-go check-ui

check-ui:
	$(NPM) run coverage
	$(NODE) scripts/quality.mjs js

check-go:
	$(NODE) scripts/quality.mjs go

check-format:
	$(NODE) scripts/quality.mjs format

check-lint:
	$(NODE) scripts/quality.mjs lint
	$(NPM) run lint
	$(NODE) scripts/quality.mjs modules

check-diagnostics:
	$(NODE) scripts/quality.mjs diagnostics

tools: tools-security hooks
	GOBIN=$(abspath .tools/bin) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
	GOBIN=$(abspath .tools/bin) $(GO) install golang.org/x/tools/gopls@v0.22.0
	GOBIN=$(abspath .tools/bin) $(GO) install golang.org/x/tools/cmd/deadcode@v0.43.0
	$(NPM) ci --ignore-scripts

.PHONY: tools-security hooks check-security test-security
tools-security:
	python3 scripts/install-gitleaks.py

hooks:
	python3 scripts/install-hooks.py

check-security:
	python3 scripts/publication.py check

test-security:
	python3 -m unittest discover -s scripts -p 'test_*.py'
