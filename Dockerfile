FROM golang:1.15

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV DIST_PATH=/bin
ENV BUILD_ARGS='-mod=readonly -ldflags="-s -w"'
RUN make

FROM gcr.io/distroless/base
COPY --from=0 /bin/urlresolver /bin/urlresolver
CMD ["/bin/urlresolver"]
