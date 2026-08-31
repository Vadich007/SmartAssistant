BINARY := bin/meetnotes
PKG := ./cmd/meetnotes

.PHONY: build up down migrate worker test test-race test-race-docker test-integration lint tidy clean

build:
	go build -o $(BINARY) $(PKG)

tidy:
	go mod tidy

up:
	docker compose up -d

down:
	docker compose down

migrate:
	go run $(PKG) migrate up

worker:
	go run $(PKG) worker

test:
	go test ./...

test-race:
	go test -race ./...

test-race-docker:
	docker run --rm -v "$(CURDIR)":/src -w /src golang:1.26 go test -race ./...

test-integration:
	go test -tags integration ./test/integration/...

lint:
	go vet ./...
	gofmt -l .

clean:
	rm -rf bin data
