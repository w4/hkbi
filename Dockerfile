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
WORKDIR /app
COPY --from=build /build/hkbi /app/hkbi
CMD ["/app/hkbi", "/data/config.toml"]
