FROM golang:1.22 as build

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -s Omit the symbol table and debug information.
# -w Omit the DWARF symbol table.
RUN CGO_ENABLED=0 go build -ldflags="-s -w"


FROM gcr.io/distroless/static-debian12:nonroot
LABEL maintainer="marius.beck@nb.no"

# dns port
EXPOSE 1053 1053/udp
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

COPY --from=build /build/veidemann-dns-resolver /usr/local/sbin/veidemann-dns-resolver
COPY --chown=nonroot:nonroot Corefile.docker /Corefile

ENTRYPOINT ["veidemann-dns-resolver"]
CMD ["-conf", "/Corefile"]
