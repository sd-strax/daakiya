# The parent directory has a go.work that does not include this module; GOWORK=off keeps builds self-contained.
export GOWORK=off

.PHONY: build test vet install clean release

build:
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty)" -o bin/daakiya ./cmd/daakiya

test:
	go test ./...

vet:
	go vet ./...

install: build
	mkdir -p $(HOME)/.local/bin && cp bin/daakiya $(HOME)/.local/bin/daakiya
	@echo "installed to $(HOME)/.local/bin/daakiya (add it to PATH if needed)"

release:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$$(git describe --tags --always)" -o dist/daakiya-darwin-arm64 ./cmd/daakiya
	GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$$(git describe --tags --always)" -o dist/daakiya-darwin-amd64 ./cmd/daakiya

clean:
	rm -rf bin dist
