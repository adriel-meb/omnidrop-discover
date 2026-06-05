# ─── omnidrop-discover — Makefile ─────────────────────────────────────────────
# Common commands for development and testing.

.PHONY: build run-serve run-scan run-probe vet test clean fmt

BINARY  = omnidrop-discover
CMD     = ./cmd/omnidrop-discover

## build — compile the binary
build:
	go build -o $(BINARY) $(CMD)

## run-serve — advertise this machine (default port 9000)
run-serve:
	go run $(CMD) --serve

## run-scan — scan LAN for omnidrop instances (default timeout 8s)
run-scan:
	go run $(CMD) --scan

## run-probe — probe a specific host:port
run-probe:
	go run $(CMD) --probe $(ADDR)

## vet — run Go's static analysis
vet:
	go vet ./...

## test — run all tests
test:
	go test ./...

## fmt — format all Go source files
fmt:
	go fmt ./...

## clean — remove built binary
clean:
	rm -f $(BINARY)

## dev-scan — fast scan with short timeout (useful during development)
dev-scan:
	go run $(CMD) --scan --timeout 2s

## dev-serve — verbose serve mode
dev-serve:
	go run $(CMD) --serve --verbose

.DEFAULT_GOAL := build
