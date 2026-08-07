#!/bin/sh
set -eu

# Automated, hardware-independent portion of the Prompt 14 acceptance suite.
# Physical and elapsed-time gates are recorded separately in the checklist.
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"

test -z "$(gofmt -l ./cmd ./internal ./pkg)"
go vet ./...
staticcheck ./...
golangci-lint run ./...
go test -race -coverprofile=coverage.out ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath ./cmd/aquaos ./cmd/aquaosctl ./cmd/aquaos-admin
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath ./cmd/aquaos ./cmd/aquaosctl ./cmd/aquaos-admin

printf '%s\n' 'Automated acceptance checks passed. Physical and 72-hour gates remain separate.'
