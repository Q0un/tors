# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o bin cmd/main.go

FROM alpine:3.16

WORKDIR /app

COPY --from=builder /build/bin ./
COPY ./configs/ /configs/

EXPOSE 6000
EXPOSE 6001
EXPOSE 6002
EXPOSE 7000
EXPOSE 7001
EXPOSE 7002

ENTRYPOINT ["./bin"]
