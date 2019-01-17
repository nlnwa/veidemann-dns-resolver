FROM golang:1.11

RUN apt-get update -qqy \
  && apt-get -qqy install unzip \
  && rm -rf /var/lib/apt/lists/* /var/cache/apt/*

COPY plugin /go/src/github.com/nlnwa/veidemann-dns-resolver/plugin
COPY iputil /go/src/github.com/nlnwa/veidemann-dns-resolver/iputil
COPY vendor /go/src/github.com/nlnwa/veidemann-dns-resolver/vendor
COPY main.go /go/src/github.com/nlnwa/veidemann-dns-resolver/main.go

RUN cd /go/src/github.com/nlnwa/veidemann-dns-resolver \
    && go test -race ./... \
    && CGO_ENABLED=0 go build -tags netgo -o /coredns


FROM alpine:latest
LABEL maintainer="Norsk nettarkiv"

EXPOSE 53 53/udp 9153 8053 8080
ENV DNS_SERVER=8.8.8.8 \
    CONTENT_WRITER_HOST=contentwriter \
    CONTENT_WRITER_PORT=8080 \
    DB_HOST=localhost \
    DB_PORT=28015 \
    DB_USER=admin \
    DB_PASSWORD=admin \
    DB_NAME=veidemann

COPY --from=0 /coredns /coredns
COPY Corefile /Corefile

ENTRYPOINT ["/coredns"]
