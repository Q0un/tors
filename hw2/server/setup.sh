#!/bin/bash

protoc -I ./proto \
  --go_out ./proto --go_opt paths=source_relative \
  --go-grpc_out ./proto --go-grpc_opt paths=source_relative \
  --grpc-gateway_out ./proto --grpc-gateway_opt paths=source_relative --experimental_allow_proto3_optional\
  ./proto/api/api.proto

protoc -I ./proto \
  --go_out ./proto --go_opt paths=source_relative \
  --go-grpc_out ./proto --go-grpc_opt paths=source_relative \
  --grpc-gateway_out ./proto --grpc-gateway_opt paths=source_relative --experimental_allow_proto3_optional\
  ./proto/raft/raft.proto

docker-compose -f ./docker-compose.yml up --build
