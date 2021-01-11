.PHONY: clean deploy deps gcloud-auth image imagepush lint run stagedeploy test testci testcover

# The version that will be used in docker tags (e.g. to push a
# urlresolver:latest image use `make imagepush VERSION=latest)`
VERSION ?= $(shell git rev-parse --short HEAD)

# Built binaries will be placed here
DIST_PATH	?= dist
BUILD_ARGS	?= -ldflags="-s -w"

# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.txt
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race

# Tool dependencies
TOOL_BIN_DIR     ?= $(shell go env GOPATH)/bin
TOOL_GOLINT      := $(TOOL_BIN_DIR)/golint
TOOL_STATICCHECK := $(TOOL_BIN_DIR)/staticcheck

GO_SOURCES = $(wildcard **/*.go)

# =============================================================================
# build
# =============================================================================
build: $(DIST_PATH)/urlmetadata

$(DIST_PATH)/urlmetadata: $(GO_SOURCES)
	mkdir -p $(DIST_PATH)
	CGO_ENABLED=0 go build -o $(DIST_PATH)/urlresolver ./cmd/urlresolver

buildtests:
	CGO_ENABLED=0 go test -ldflags="-s -w" -v -c -o $(DIST_PATH)/urlresolver.test ./httpbin

clean:
	rm -rf $(DIST_PATH) $(COVERAGE_PATH)

# =============================================================================
# test & lint
# =============================================================================
test:
	go test $(TEST_ARGS) ./...

# Test command to run for continuous integration, which includes code coverage
# based on codecov.io's documentation:
# https://github.com/codecov/example-go/blob/b85638743b972bd0bd2af63421fe513c6f968930/README.md
testci: build
	go test $(TEST_ARGS) $(COVERAGE_ARGS) ./...

testcover: testci
	go tool cover -html=$(COVERAGE_PATH)

lint: $(TOOL_GOLINT) $(TOOL_STATICCHECK)
	test -z "$$(gofmt -d -s -e .)" || (echo "Error: gofmt failed"; gofmt -d -s -e . ; exit 1)
	go vet ./...
	$(TOOL_GOLINT) -set_exit_status ./...
	$(TOOL_STATICCHECK) ./...


# =============================================================================
# run locally
# =============================================================================
run: build
	$(DIST_PATH)/urlresolver

watch: $(TOOL_REFLEX)
	reflex -s -r '\.(go)$$' make run


# =============================================================================
# deploy to fly.io
# =============================================================================
deploy:
	flyctl deploy --strategy=rolling


# =============================================================================
# docker images
# =============================================================================
image:
	docker build -t $(DOCKER_TAG_DOCKERHUB) -t $(DOCKER_TAG_GCLOUD) .

imagepush: image
	docker push $(DOCKER_TAG_GCLOUD)
	docker push $(DOCKER_TAG_GCLOUD)


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
