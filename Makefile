VERSION ?= $(shell git rev-parse --short HEAD)

# Built binaries will be placed here
DIST_PATH	?= dist
BUILD_ARGS	?= -ldflags="-s -w"

# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.out
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race

# Tool dependencies
TOOL_BIN_DIR     ?= $(shell go env GOPATH)/bin
TOOL_GOLINT      := $(TOOL_BIN_DIR)/golint
TOOL_REFLEX      := $(TOOL_BIN_DIR)/reflex
TOOL_STATICCHECK := $(TOOL_BIN_DIR)/staticcheck

# =============================================================================
# build
# =============================================================================
$(DIST_PATH)/urlresolver: $(shell find . -name '*.go')
	mkdir -p $(DIST_PATH)
	CGO_ENABLED=0 go build -o $(DIST_PATH)/urlresolver ./cmd/urlresolver

build: $(DIST_PATH)/urlresolver
.PHONY: build

clean:
	rm -rf $(DIST_PATH) $(COVERAGE_PATH)
.PHONY: clean

# =============================================================================
# test & lint
# =============================================================================
test:
	go test $(TEST_ARGS) ./...
.PHONY: test

# Test command to run for continuous integration, which includes code coverage
# based on codecov.io's documentation:
# https://github.com/codecov/example-go/blob/b85638743b972bd0bd2af63421fe513c6f968930/README.md
testci: build
	go test $(TEST_ARGS) $(COVERAGE_ARGS) ./...
.PHONY: testci

testcover: testci
	go tool cover -html=$(COVERAGE_PATH)
.PHONY: testcover

lint: $(TOOL_GOLINT) $(TOOL_STATICCHECK)
	test -z "$$(gofmt -d -s -e .)" || (echo "Error: gofmt failed"; gofmt -d -s -e . ; exit 1)
	go vet ./...
	$(TOOL_GOLINT) -set_exit_status ./...
	$(TOOL_STATICCHECK) ./...
.PHONY: lint

# =============================================================================
# run locally
# =============================================================================
run: build
	$(DIST_PATH)/urlresolver
.PHONY: run

watch: $(TOOL_REFLEX)
	reflex -s -r '\.(go)$$' make run
.PHONY: watch

# =============================================================================
# docker
# =============================================================================
buildimage:
	docker build --tag="urlresolver:$(VERSION)" .
.PHONY: image

rundocker: buildimage
	docker run --rm -p 8080:8080 -e PORT=8080 urlresolver:$(VERSION)
.PHONY: rundocker

# =============================================================================
# deploy to fly.io
# =============================================================================
deploy:
	flyctl deploy --strategy=rolling
.PHONY: deploy

# =============================================================================
# dependencies
#
# Deps are installed outside of working dir to avoid polluting go modules
# =============================================================================
$(TOOL_GOLINT):
	cd /tmp && go get -u golang.org/x/lint/golint

$(TOOL_REFLEX):
	cd /tmp && go get -u github.com/cespare/reflex

$(TOOL_STATICCHECK):
	cd /tmp && go get -u honnef.co/go/tools/cmd/staticcheck
