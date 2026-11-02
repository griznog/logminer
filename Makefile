BINARY := logmetrics

.PHONY: build test clean

# Build the binary into the repository root.
build:
	go build -o $(BINARY) .

# Vet the code and run all tests.
test:
	go vet ./...
	go test ./...

# Remove the binary and cached test results.
clean:
	go clean -testcache
	rm -f $(BINARY)
