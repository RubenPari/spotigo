.PHONY: test lint build cover

test:
	go test -race ./...

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

build:
	go build -o bin/spotigo ./cmd/spotigo

cover:
	go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
