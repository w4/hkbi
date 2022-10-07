# syntax=docker/dockerfile:1
FROM golang:1.19.2-bullseye AS build
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN go build -o hkbi cmd/hkbi/main.go

FROM debian:bullseye
LABEL org.opencontainers.image.source="https://github.com/w4/hkbi"
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get -y install ffmpeg && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /build/hkbi /app/hkbi
CMD ["/app/hkbi", "/data/config.toml"]
