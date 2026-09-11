.PHONY: build test lint sync
build:
	go build -o bin/choco-internalizer ./cmd/choco-internalizer
test:
	go test -race ./...
lint:
	go vet ./...
sync: build
	./scripts/sync-packages.sh packages.txt
