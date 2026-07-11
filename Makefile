# HeroPanel build pipeline.
#
# The React SPA is embedded into hpd (web/embed.go), so the frontend must be
# built BEFORE `go build` for the real UI to be served. `go build` still works
# without a frontend build (a placeholder page is served) thanks to the
# web/dist/.gitkeep placeholder.

BIN := bin
GOFLAGS := -trimpath

.PHONY: all dist web build test race vet fmt run clean tidy

all: build

## dist: build the frontend then the Go binaries (full release build)
dist: web build

## web: install deps and build the SPA into web/dist
web:
	npm --prefix web install --no-audit --no-fund
	npm --prefix web run build

## build: compile the Go binaries into ./bin
build:
	go build $(GOFLAGS) -o $(BIN)/hpd ./cmd/hpd
	go build $(GOFLAGS) -o $(BIN)/hp-broker ./cmd/hp-broker
	go build $(GOFLAGS) -o $(BIN)/hpctl ./cmd/hpctl 2>/dev/null || true

## test: run all Go tests
test:
	go test ./...

## race: run tests with the race detector (requires cgo / a C compiler)
race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

## run: run hpd from source (dev)
run:
	go run ./cmd/hpd

## tidy: tidy go modules
tidy:
	go mod tidy

clean:
	rm -rf $(BIN)
	rm -rf web/dist/assets web/dist/index.html
