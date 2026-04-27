.PHONY: test cover lint bench build clean

test:
	go test ./... -race -timeout 60s

cover:
	go test ./... -coverprofile=cover.out
	go tool cover -func=cover.out | tail -1

lint:
	@which golangci-lint > /dev/null || (echo "install: brew install golangci-lint"; exit 1)
	golangci-lint run

bench:
	go test ./internal/session/ -bench=. -benchtime=5s -run=^$$

build:
	go build -o csess ./cmd/csess

clean:
	rm -f csess cover.out
