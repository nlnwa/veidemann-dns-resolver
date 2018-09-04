FROM golang:1.11

RUN apt-get update -qqy \
  && apt-get -qqy install unzip \
  && rm -rf /var/lib/apt/lists/* /var/cache/apt/*

RUN go get -v github.com/coredns/coredns
RUN cd /go/src/github.com/coredns/coredns

COPY scripts/protobuf-tools.sh /protobuf-tools.sh
RUN cd / \
    && ./protobuf-tools.sh

COPY plugin /go/src/github.com/nlnwa/veidemann-dns-resolver/plugin
COPY scripts/build-protobuf.sh /go/src/github.com/nlnwa/veidemann-dns-resolver/scripts/build-protobuf.sh

RUN cd /go/src/github.com/nlnwa/veidemann-dns-resolver \
    && ln -s /tools . \
    && scripts/build-protobuf.sh \
    && go get gopkg.in/gorethink/gorethink.v4 \
    && CGO_ENABLED=1 go test -race -v -tags netgo ./test ./plugin/...

WORKDIR /go/src/github.com/coredns/coredns

RUN sed -i -e "/cache:cache/a archiver:github.com/nlnwa/veidemann-dns-resolver/plugin/archiver" plugin.cfg \
    && sed -i -e "/prometheus:metrics/a resolve:github.com/nlnwa/veidemann-dns-resolver/plugin/resolve" plugin.cfg
#RUN sed -i -e "/cache:cache/a archiver:archiver" plugin.cfg \
#    && sed -i -e "/prometheus:metrics/a resolve:resolve" plugin.cfg \
#    && go generate && CGO_ENABLED=0 go install -tags netgo

#RUN rm -rf vendor/golang.org/x/net plugin/bind/README.md
RUN make


FROM alpine:latest
LABEL maintainer="Norsk nettarkiv"

USER 1001
EXPOSE 1053 1053/udp 9153 8053
ENV DNS_SERVER=8.8.8.8 \
    CONTENT_WRITER_HOST=contentwriter \
    CONTENT_WRITER_PORT=8080 \
    DB_HOST=localhost \
    DB_PORT=28015 \
    DB_USER=admin \
    DB_PASSWORD=admin

COPY --from=0 /go/bin/coredns /coredns
COPY Corefile /Corefile

ENTRYPOINT ["/coredns"]
