FROM golang:1.15 as build

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go test ./...
RUN CGO_ENABLED=0 go build -tags netgo


FROM gcr.io/distroless/base
LABEL maintainer="Norsk nettarkiv"

# dns port
EXPOSE 53 53/udp
# veidemann.api.dnsresolver.Resolve
EXPOSE 8053
# prometheus metrics
EXPOSE 9153
# readiness
EXPOSE 8181

ENV DNS_SERVER=8.8.8.8 \
    CONTENT_WRITER_HOST=veidemann-contentwriter \
    CONTENT_WRITER_PORT=8080 \
    LOG_WRITER_HOST=veidemann-log-service \
    LOG_WRITER_PORT=8080

COPY --from=0 /build/veidemann-dns-resolver /veidemann-dns-resolver
COPY Corefile.docker /Corefile

ENTRYPOINT ["/veidemann-dns-resolver"]
