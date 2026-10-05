# SMB OS Desktop: runtime build targets. Requires Go (see go.mod) on PATH.
GO      ?= go
VERSION ?= 0.1.0-dev

.PHONY: build test race vet fmt check release run clean

build: ## build aios for this computer into bin/
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/aios ./cmd/aios

test: ## run the tests
	$(GO) test -count=1 ./...

race: ## run the tests with the race detector
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w cmd internal packs web/embed.go

check: vet race ## what CI runs
	@test -z "$$(gofmt -l cmd internal packs web/embed.go)" || (gofmt -l cmd internal packs web/embed.go; exit 1)

release: ## build every platform into dist/: make release VERSION=0.1.0
	bash scripts/build_release.sh $(VERSION)

run: build ## build and start with a throwaway data folder
	./bin/aios --data "$$(mktemp -d)"

clean:
	rm -rf bin dist
