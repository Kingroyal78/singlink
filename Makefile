NAME = singlink
COMMIT = $(shell git rev-parse --short HEAD)
TAGS ?= $(shell cat release/DEFAULT_BUILD_TAGS_OTHERS)

GOHOSTOS = $(shell go env GOHOSTOS)
GOHOSTARCH = $(shell go env GOHOSTARCH)
VERSION=$(shell CGO_ENABLED=0 GOOS=$(GOHOSTOS) GOARCH=$(GOHOSTARCH) go run ./cmd/internal/read_tag)

LDFLAGS_SHARED = $(shell cat release/LDFLAGS)
PARAMS = -v -trimpath -ldflags "-X 'github.com/singlink/singlink/constant.Version=$(VERSION)' $(LDFLAGS_SHARED) -s -w -buildid="
MAIN_PARAMS = $(PARAMS) -tags "$(TAGS)"
MAIN = ./cmd/singlink
PREFIX ?= $(shell go env GOPATH)

.PHONY: test release docs build

build:
	export GOTOOLCHAIN=local && \
	go build $(MAIN_PARAMS) $(MAIN)

race:
	export GOTOOLCHAIN=local && \
	go build -race $(MAIN_PARAMS) $(MAIN)

ci_build:
	export GOTOOLCHAIN=local && \
	go build $(PARAMS) $(MAIN) && \
	go build $(MAIN_PARAMS) $(MAIN)

generate_completions:
	go run -v --tags "$(TAGS),generate,generate_completions" $(MAIN)

install:
	go build -o $(PREFIX)/bin/$(NAME) $(MAIN_PARAMS) $(MAIN)

fmt:
	@golangci-lint fmt

fmt_docs:
	go run ./cmd/internal/format_docs

lint:
	GOOS=linux golangci-lint run ./...
	GOOS=windows golangci-lint run ./...
	GOOS=darwin golangci-lint run ./...
#	GOOS=freebsd golangci-lint run ./...

lint_install:
	go install -v github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

proto:
	@go run ./cmd/internal/protogen
	@gofumpt -l -w .
	@gofumpt -l -w .

proto_install:
	go install -v google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install -v google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

update_certificates:
	go run ./cmd/internal/update_certificates

release:
	goreleaser release --clean --skip publish
	mkdir dist/release
	mv dist/*.tar.gz \
		dist/*.zip \
		dist/*.deb \
		dist/*.rpm \
		dist/*_amd64.pkg.tar.zst \
		dist/*_arm64.pkg.tar.zst \
		dist/release
	ghr --replace --draft --prerelease -p 5 "v${VERSION}" dist/release
	rm -r dist/release

release_repo:
	goreleaser release -f .goreleaser.fury.yaml --clean

release_install:
	go install -v github.com/tcnksm/ghr@latest

test:
	@go test -v ./... && \
	cd test && \
	go mod tidy && \
	go test -v -tags "$(TAGS_TEST)" .

test_stdio:
	@go test -v ./... && \
	cd test && \
	go mod tidy && \
	go test -v -tags "$(TAGS_TEST),force_stdio" .

docs:
	venv/bin/mkdocs serve

publish_docs:
	venv/bin/mkdocs gh-deploy -m "Update" --force --ignore-version --no-history

docs_install:
	python3 -m venv venv
	source ./venv/bin/activate && pip install --force-reinstall mkdocs-material=="9.7.2" mkdocs-static-i18n=="1.2.*"

clean:
	rm -rf bin dist singlink
	rm -f $(shell go env GOPATH)/singlink

update:
	git fetch
	git reset FETCH_HEAD --hard
	git clean -fdx

%:
	@:
