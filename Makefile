BINARY  := fleet
CMD     := ./cmd/fleet

.PHONY: build test lint vet check clean

build:
	go build -o $(BINARY) $(CMD)

test:
	go test -short ./...

vet:
	go vet ./...

lint: vet
	@if command -v staticcheck >/dev/null 2>&1; then staticcheck ./...; fi

check: lint test build

clean:
	rm -f $(BINARY)
