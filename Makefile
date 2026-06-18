BIN := codegraph
PKG := ./cmd/codegraph

.PHONY: build test tidy run-index run-mcp clean

build:
	go build -o $(BIN) $(PKG)

test:
	go test ./...

tidy:
	go mod tidy

# make run-index REPO=/path/to/repo
run-index: build
	./$(BIN) index $(REPO)

# make run-mcp REPO=/path/to/repo
run-mcp: build
	./$(BIN) mcp $(REPO)

clean:
	rm -f $(BIN) $(BIN).exe
