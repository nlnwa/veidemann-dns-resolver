#!/usr/bin/env bash

API_VERSION=0.1.8

rm -rf protobuf veidemann_api
mkdir protobuf veidemann_api

wget -O - -q https://github.com/nlnwa/veidemann-api/archive/${API_VERSION}.tar.gz | tar --strip-components=2 -zx -C protobuf
cp -r tools/include/google/* google/

tools/bin/protoc -Iprotobuf --go_out=plugins=grpc:veidemann_api protobuf/*.proto
sed -i -e s?golang.org/x/net/context?context? veidemann_api/*