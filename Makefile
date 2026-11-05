# Envry development tasks. Requires GNU Make 4+, Go 1.25+ and Node.js 20+.
# On Windows, run these with `mingw32-make` (or any GNU Make); recipes work
# in both sh and cmd.exe.

ifeq ($(OS),Windows_NT)
EXE := .exe
endif

BIN  := bin/envry$(EXE)
AIR  := github.com/air-verse/air@v1.62.0
RACE ?= -race

# Kept in a variable: backslash-newline in a recipe breaks under cmd.exe.
AIR_FLAGS := \
	-build.cmd "go build -o tmp/envry$(EXE) ./cmd/envry" \
	-build.bin "tmp/envry$(EXE)" \
	-build.args_bin "serve" \
	-build.include_ext "go" \
	-build.exclude_dir "bin,docs,tmp,web,internal/webui/dist" \
	-misc.clean_on_exit "true"

.DEFAULT_GOAL := build
.PHONY: dev dev-go dev-web build ui test lint fmt

# Go server with hot reload + Vite dev server (open http://localhost:5173).
# Vite proxies /api and /ws to the Go server on :8025.
dev: web/node_modules
	$(MAKE) -j2 dev-go dev-web

dev-go:
	go run $(AIR) $(AIR_FLAGS)

dev-web: web/node_modules
	cd web && npm run dev

# Single binary with the web UI embedded.
build: ui
	go build -trimpath -o $(BIN) ./cmd/envry

ui: web/node_modules
	cd web && npm run build

test: web/node_modules
	go test $(RACE) ./...
	cd web && npm run typecheck

lint: web/node_modules
	go vet ./...
	cd web && npm run lint

fmt:
	gofmt -w cmd internal

web/node_modules: web/package-lock.json
	cd web && npm ci
