# net-healthcheck (Go). `make help` lists the targets.
BIN      := net-healthcheck
PKG      := ./cmd/net-healthcheck
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
DIST     := dist

.PHONY: help fmt-check vet test build cross docker clean

help: ## show this help
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

fmt-check: ## fail if any file is not gofmt-formatted
	@test -z "$$(gofmt -l .)" || (echo "not formatted:"; gofmt -l .; exit 1)

vet: ## go vet
	go vet ./...

test: ## tests with the race detector and coverage
	go test -race -cover -count=1 ./...

build: ## build for this machine into dist/
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BIN) $(PKG)

cross: ## static binaries for linux/amd64, linux/arm64, darwin/arm64
	@for t in linux/amd64 linux/arm64 darwin/arm64; do \
	  os=$${t%/*}; arch=$${t#*/}; \
	  echo "building $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BIN)-$$os-$$arch $(PKG) || exit 1; \
	done
	@ls -l $(DIST)

docker: ## small image (multi-stage, static binary, non-root)
	docker build -t net-healthcheck-go:$(VERSION) --build-arg VERSION=$(VERSION) .

clean:
	rm -rf $(DIST)
