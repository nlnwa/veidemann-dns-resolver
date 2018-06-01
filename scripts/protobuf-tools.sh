#!/usr/bin/env bash

mkdir -p tools
cd tools
wget -q https://github.com/google/protobuf/releases/download/v3.5.1/protoc-3.5.1-linux-x86_64.zip
unzip protoc-3.5.1-linux-x86_64.zip
rm protoc-3.5.1-linux-x86_64.zip

go get google.golang.org/grpc
go get github.com/golang/protobuf/protoc-gen-go
go get github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger/options
go get google.golang.org/genproto/googleapis/api/annotations
