GO ?= go
BINARY ?= s3ctl
CMD_PATH ?= ./cmd/$(BINARY)
PKG ?= github.com/soakes/s3ctl
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf '%s' dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf '%s' packaged)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOFLAGS ?= -trimpath
SHASUM ?= shasum -a 256
RELEASE_TARGETS ?= linux/amd64 linux/arm64 linux/arm/v7 darwin/amd64 darwin/arm64
BINARY_PATH ?=
DEB_ARCH ?=
GO_TOOLCHAIN_VERSION ?=
GOLANGCI_LINT ?= $(CURDIR)/bin/golangci-lint
GOLANGCI_LINT_VERSION ?= $(shell tr -d '[:space:]' < .golangci-lint-version 2>/dev/null)
LDFLAGS ?= -s -w \
	-X $(PKG)/internal/buildinfo.Version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(PKG)/internal/buildinfo.BuildDate=$(BUILD_DATE)

.PHONY: fmt fmt-check lint lint-fix lint-install vet test build build-cross build-release package-deb docker-build website-install website-build website-check website-capture refresh-go-toolchain clean

fmt:
	gofmt -w cmd internal
	@if [ -x "$(GOLANGCI_LINT)" ]; then \
		"$(GOLANGCI_LINT)" fmt; \
	else \
		printf 'note: skipping golangci-lint formatters; run `make lint-install` to enable gofumpt and goimports\n'; \
	fi

fmt-check:
	test -z "$$(gofmt -l cmd internal)"

lint-install:
	test -n "$(GOLANGCI_LINT_VERSION)"
	bash scripts/install-golangci-lint.sh "$(GOLANGCI_LINT_VERSION)" "$(CURDIR)/bin"

lint:
	test -x "$(GOLANGCI_LINT)" || (printf 'missing %s; run `make lint-install`\n' "$(GOLANGCI_LINT)" >&2 && exit 1)
	"$(GOLANGCI_LINT)" run

lint-fix:
	test -x "$(GOLANGCI_LINT)" || (printf 'missing %s; run `make lint-install`\n' "$(GOLANGCI_LINT)" >&2 && exit 1)
	"$(GOLANGCI_LINT)" fmt
	"$(GOLANGCI_LINT)" run --fix

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

build:
	mkdir -p dist
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o dist/$(BINARY) $(CMD_PATH)

build-cross:
	test -n "$(GOOS)"
	test -n "$(GOARCH)"
	test -n "$(OUTPUT)"
	mkdir -p $$(dirname "$(OUTPUT)")
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) \
		$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(OUTPUT) $(CMD_PATH)

build-release:
	rm -rf dist/release
	mkdir -p dist/release
	set -eu; \
	for target in $(RELEASE_TARGETS); do \
		goos=$${target%%/*}; \
		rest=$${target#*/}; \
		goarch=$${rest%%/*}; \
		goarm=""; \
		suffix="$${goos}-$${goarch}"; \
		if [ "$${rest}" != "$${goarch}" ]; then \
			goarm=$${rest#*/}; \
			goarm=$${goarm#v}; \
			suffix="$${suffix}v$${goarm}"; \
		fi; \
		output="dist/release/$(BINARY)-$${suffix}"; \
		$(MAKE) build-cross GOOS="$${goos}" GOARCH="$${goarch}" GOARM="$${goarm}" OUTPUT="$${output}"; \
		tar -C dist/release -czf "$${output}.tar.gz" "$(BINARY)-$${suffix}"; \
	done
	cd dist/release && $(SHASUM) *.tar.gz > $(BINARY)_SHA256SUMS

package-deb:
	test -n "$(BINARY_PATH)"
	test -n "$(DEB_ARCH)"
	mkdir -p dist
	bash scripts/build-deb-package.sh "$(BINARY_PATH)" dist "$(VERSION)" "$(DEB_ARCH)"

docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg VCS_REF=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(BINARY):dev \
		.

website-install:
	npm --prefix website install

website-build:
	test -d website/node_modules || (printf 'missing website dependencies; run `make website-install`\n' >&2 && exit 1)
	npm --prefix website run build

website-check:
	test -d website/node_modules || (printf 'missing website dependencies; run `make website-install`\n' >&2 && exit 1)
	npm --prefix website run check

website-capture:
	test -d website/node_modules || (printf 'missing website dependencies; run `make website-install`\n' >&2 && exit 1)
	npm --prefix website run capture

refresh-go-toolchain:
	GO_TOOLCHAIN_VERSION="$(GO_TOOLCHAIN_VERSION)" bash scripts/refresh-go-toolchain.sh

clean:
	rm -rf dist
