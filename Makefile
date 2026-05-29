BINARY := kms-wrapper

.PHONY: build test lint dev-up dev-down run-gateway

build:
	go build -o bin/$(BINARY) ./cmd/kms-wrapper

test:
	go test ./...

lint:
	golangci-lint run ./...

dev-up:
	docker compose up -d

dev-down:
	docker compose down

run-gateway:
	go run ./cmd/kms-wrapper serve
