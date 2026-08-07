.PHONY: bootstrap build build-all test lint vet staticcheck fmt run simulate clean

BINARY := bin/aquaos

build:
	go build -trimpath -o $(BINARY) ./cmd/aquaos

build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/aquaos-linux-amd64 ./cmd/aquaos
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/aquaos-healthcheck-linux-amd64 ./cmd/healthcheck
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/aquaos-sim-linux-amd64 ./cmd/aquaos-sim
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/aquaos-linux-arm64 ./cmd/aquaos
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/aquaos-healthcheck-linux-arm64 ./cmd/healthcheck
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/aquaos-sim-linux-arm64 ./cmd/aquaos-sim

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

staticcheck:
	staticcheck ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find cmd internal pkg tests -name '*.go' 2>/dev/null)

run:
	go run ./cmd/aquaos -config configs/aquaos.yaml

simulate:
	go run ./cmd/aquaos-sim -scenario configs/scenarios/normal-temperature.json

bootstrap:
	go run ./cmd/devbootstrap -config configs/development.yaml

clean:
	go clean
	rm -f $(BINARY) coverage.out
