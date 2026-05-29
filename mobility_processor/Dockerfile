# Processor Dockerfile for Mobility Project

FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /usr/local/bin/processor ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app/cmd/server
COPY --from=builder /usr/local/bin/processor /usr/local/bin/processor
EXPOSE 8080
CMD ["/usr/local/bin/processor"]