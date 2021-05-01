# syntax = docker/dockerfile:1-experimental
FROM golang:1.16

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    BUILD_ARGS='-mod=readonly -ldflags="-s -w"' \
    DIST_PATH=/bin \
    make

FROM gcr.io/distroless/base
COPY --from=0 /bin/urlresolver /bin/urlresolver
CMD ["/bin/urlresolver"]
