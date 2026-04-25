.PHONY: help \
        proto proto-buf \
        web web-install web-typecheck web-test web-preview \
        build build-debug build-dev build-demo install \
        dev-go dev-web dev-repl \
        run run-web run-repl \
        test test-race test-cover test-web test-agent \
        vet lint fmt fix tidy verify \
        ci clean

# ── Variables ────────────────────────────────────────────────────────
BINARY   := feino
WEB_DIR  := internal/web/ui
GOBIN    := $(shell go env GOPATH)/bin

WEB_HOST ?= 127.0.0.1
WEB_PORT ?= 7700

# Coverage output
COVER_OUT  := coverage.out
COVER_HTML := coverage.html

# Package list with gitignore-style exclusions baked in.
# Excluded: node_modules/ — npm-vendored Go subtrees (e.g. flatted/golang/...)
#          that ship inside frontend deps and aren't part of feino.
# Use $(GO_PKGS) anywhere `./...` would otherwise expand into ignored paths.
GO_IGNORE_PATTERN := /node_modules/
GO_PKGS := $(shell go list ./... 2>/dev/null | grep -v -E '$(GO_IGNORE_PATTERN)')

.DEFAULT_GOAL := help

# ── Help ─────────────────────────────────────────────────────────────
help:
	@echo ""
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build"
	@echo "  build          Production binary (proto + web embedded, -tags web)"
	@echo "  build-debug    Debug binary — no inlining or optimisation"
	@echo "  build-dev      Quick binary without embedded frontend (for Go-only work)"
	@echo "  build-demo     Build feino + demoserver (for VHS demo GIF generation)"
	@echo "  install        Install binary to \$$GOPATH/bin"
	@echo ""
	@echo "Development"
	@echo "  dev-go         Run web server on port \$$WEB_PORT (default 7700)"
	@echo "  dev-web        Run Vite dev server (proxies API to :\$$WEB_PORT)"
	@echo "  run            Run TUI (requires ANTHROPIC_API_KEY or config)"
	@echo "  run-web        Run web server with embedded frontend"
	@echo "  run-repl       Run plain stdin/stdout REPL"
	@echo ""
	@echo "Proto"
	@echo "  proto          Regenerate with protoc"
	@echo "  proto-buf      Regenerate with buf"
	@echo ""
	@echo "Frontend"
	@echo "  web            Full frontend build: npm ci + tsc + vite build"
	@echo "  web-install    Install frontend dependencies (npm install)"
	@echo "  web-typecheck  Type-check TypeScript without emitting (tsc -b --noEmit)"
	@echo "  web-test       Run frontend unit tests with vitest"
	@echo "  web-preview    Serve the production frontend build locally"
	@echo ""
	@echo "Tests"
	@echo "  test           Run all Go tests"
	@echo "  test-race      Run all Go tests with race detector"
	@echo "  test-cover     Run tests and open HTML coverage report"
	@echo "  test-web       Run internal/web package tests with race detector"
	@echo "  test-agent     Run internal/agent package tests with race detector"
	@echo ""
	@echo "Code quality"
	@echo "  vet            go vet ./..."
	@echo "  lint           go vet + staticcheck + golangci-lint (if installed)"
	@echo "  fmt            go fmt + goimports (if installed)"
	@echo "  fix            go fix ./..."
	@echo "  tidy           go mod tidy"
	@echo "  verify         go mod verify"
	@echo ""
	@echo "  ci             Full CI: proto + web + test-race + lint + build"
	@echo "  clean          Remove build artefacts and generated files"
	@echo ""

# ── Proto generation ─────────────────────────────────────────────────
proto:
	mkdir -p gen/feino/v1/feinov1connect
	protoc \
		-I proto \
		-I /usr/include \
		--go_out=gen \
		--go_opt=paths=source_relative \
		--connect-go_out=gen \
		--connect-go_opt=paths=source_relative \
		proto/feino/v1/feino.proto

proto-buf:
	buf generate

# ── Frontend ─────────────────────────────────────────────────────────
web:
	cd $(WEB_DIR) && npm ci && npm run build

web-install:
	cd $(WEB_DIR) && npm install

web-typecheck:
	cd $(WEB_DIR) && npx tsc -b --noEmit

web-test:
	cd $(WEB_DIR) && npm test -- --run

web-preview:
	cd $(WEB_DIR) && npm run preview

# ── Build ─────────────────────────────────────────────────────────────
build: proto web
	go build -tags web -o $(BINARY) ./cmd/feino

build-debug:
	go build -gcflags="all=-N -l" -o $(BINARY) ./cmd/feino

build-dev:
	go build -o $(BINARY) ./cmd/feino

build-demo:
	go build -o $(BINARY) ./cmd/feino
	go build -o demoserver ./cmd/demoserver

install:
	go install -tags web ./cmd/feino

# ── Development servers ───────────────────────────────────────────────
dev-go: build-dev
	./$(BINARY) --web --web-host $(WEB_HOST) --web-port $(WEB_PORT)

dev-web:
	cd $(WEB_DIR) && npm run dev

# ── Run shortcuts ────────────────────────────────────────────────────
run: build-dev
	./$(BINARY)

run-web: build-dev
	./$(BINARY) --web --web-host $(WEB_HOST) --web-port $(WEB_PORT)

run-repl: build-dev
	./$(BINARY) --no-tui

# ── Tests ─────────────────────────────────────────────────────────────
test:
	go test $(GO_PKGS)

test-race:
	go test -race $(GO_PKGS)

test-cover:
	go test -coverprofile=$(COVER_OUT) $(GO_PKGS)
	go tool cover -html=$(COVER_OUT) -o $(COVER_HTML)
	@echo "Coverage report: $(COVER_HTML)"

test-web:
	go test -race ./internal/web/...

test-agent:
	go test -race ./internal/agent/...

# ── Code quality ─────────────────────────────────────────────────────
vet:
	go vet $(GO_PKGS)

lint: vet
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck $(GO_PKGS); \
	else \
		echo "staticcheck not found — run: go install honnef.co/go/tools/cmd/staticcheck@latest"; \
	fi
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run $(GO_PKGS); \
	else \
		echo "golangci-lint not found — see https://golangci-lint.run/usage/install/"; \
	fi

fmt:
	go fmt $(GO_PKGS)
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w $$(find . -name '*.go' -not -path './gen/*' -not -path '*/node_modules/*'); \
	else \
		echo "goimports not found — run: go install golang.org/x/tools/cmd/goimports@latest"; \
	fi

fix:
	go fix $(GO_PKGS)

tidy:
	go mod tidy

verify:
	go mod verify

# ── CI ────────────────────────────────────────────────────────────────
ci: proto web
	go test -race $(GO_PKGS)
	go vet $(GO_PKGS)
	@if command -v staticcheck >/dev/null 2>&1; then staticcheck $(GO_PKGS); fi
	go build -tags web ./cmd/feino

# ── Clean ─────────────────────────────────────────────────────────────
clean:
	rm -f $(BINARY) demoserver
	rm -f $(COVER_OUT) $(COVER_HTML)
	rm -rf $(WEB_DIR)/dist
	rm -rf gen
