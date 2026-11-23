.PHONY: test test-race build build-mcp run run-mcp mcp-smoke

test:
	go test ./...

test-race:
	go test -race ./...

build:
	go build -o bin/api ./cmd/api

build-mcp:
	go build -o bin/mcp ./cmd/mcp

run:
	go run ./cmd/api

run-mcp:
	go run ./cmd/mcp

# Requires API on NAMEPIQ_API_BASE (default :8080)
mcp-smoke: build-mcp
	go run ./cmd/mcp-smoke
