FROM golang:1.16

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN BUILD_ARGS='-mod=readonly -ldflags="-s -w"' \
    DIST_PATH=/bin \
    make

FROM gcr.io/distroless/base
COPY --from=0 /bin/urlresolver /bin/urlresolver
CMD ["/bin/urlresolver"]
