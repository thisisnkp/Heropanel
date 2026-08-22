# NexPanel build pipeline.
#
# The React SPA is embedded into npd (web/embed.go), so the frontend must be
# built BEFORE `go build` for the real UI to be served. `go build` still works
# without a frontend build (a placeholder page is served) thanks to the
# web/dist/.gitkeep placeholder.

BIN := bin
GOFLAGS := -trimpath

.PHONY: all dist web build test race vet fmt run dev-api clean tidy

all: build

## dist: build the frontend then the Go binaries (full release build)
dist: web build

## web: install deps and build the SPA into web/dist
web:
	npm --prefix web install --no-audit --no-fund
	npm --prefix web run build

## build: compile the Go binaries into ./bin
build:
	go build $(GOFLAGS) -o $(BIN)/npd ./cmd/npd
	go build $(GOFLAGS) -o $(BIN)/np-broker ./cmd/np-broker
	go build $(GOFLAGS) -o $(BIN)/npctl ./cmd/npctl 2>/dev/null || true

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

## run: run npd from source (dev)
run:
	go run ./cmd/npd

## dev-api: run npd for `vite dev` — SQLite, loopback, port 8443
##
## The Vite dev server proxies /api to 127.0.0.1:8443; this is what listens
## there. SQLite because a workstation has no MariaDB, and the default driver
## with an empty DSN boots npd with no datastore at all — which the panel
## reports as `configured: false` and every login then fails.
## Same thing as `npm --prefix web run dev:api`.
dev-api:
	NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=8443 NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=$(CURDIR)/np.db go run ./cmd/npd

## tidy: tidy go modules
tidy:
	go mod tidy

clean:
	rm -rf $(BIN)
	rm -rf web/dist/assets web/dist/index.html
