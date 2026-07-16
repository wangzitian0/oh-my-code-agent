.PHONY: build test lint fixtures cover

build:
	go build ./...

test:
	go test ./... -race -coverprofile=coverage.out

cover: test
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

fixtures:
	go test ./... -run Fixture -v
