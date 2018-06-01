FROM golang:1.10

RUN apt-get update -qqy \
  && apt-get -qqy install unzip \
  && rm -rf /var/lib/apt/lists/* /var/cache/apt/*

RUN go get github.com/coredns/coredns \
    && cd /go/src/github.com/coredns/coredns \
    && make

COPY scripts/protobuf-tools.sh /protobuf-tools.sh
RUN cd / && ./protobuf-tools.sh

COPY plugin /go/src/github.com/nlnwa/veidemann-dns-resolver/plugin
COPY scripts/build-protobuf.sh /go/src/github.com/nlnwa/veidemann-dns-resolver/scripts/build-protobuf.sh

RUN cd /go/src/github.com/nlnwa/veidemann-dns-resolver \
    && ln -s /tools . \
    && scripts/build-protobuf.sh

WORKDIR /go/src/github.com/coredns/coredns

RUN sed -i -e "/cache:cache/a archiver:github.com/nlnwa/veidemann-dns-resolver/plugin/archiver" plugin.cfg \
    && sed -i -e "/prometheus:metrics/a resolve:github.com/nlnwa/veidemann-dns-resolver/plugin/resolve" plugin.cfg \
    && go generate && CGO_ENABLED=0 go install -tags netgo



FROM alpine:latest
LABEL maintainer="Norsk nettarkiv"

USER 1001
EXPOSE 1053 1053/udp 9153 8053
ENV DNS_SERVER=8.8.8.8 \
    CONTENTWRITER_HOST=contentwriter \
    CONTENTWRITER_PORT=8080

COPY --from=0 /go/bin/coredns /coredns
COPY Corefile /Corefile

ENTRYPOINT ["/coredns"]
