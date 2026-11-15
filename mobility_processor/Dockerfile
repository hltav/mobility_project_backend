# Processor Dockerfile for Mobility Project

FROM golang:1.26.2-alpine AS processor-tools
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

FROM processor-tools AS builder
COPY . .
RUN go build -o /usr/local/bin/processor ./cmd/server

FROM alpine:3.19 AS server-runtime
RUN apk add --no-cache ca-certificates
WORKDIR /app/cmd/server
COPY --from=builder /usr/local/bin/processor /usr/local/bin/processor
EXPOSE 8080
CMD ["/usr/local/bin/processor"]


FROM processor-tools AS gtfs-builder
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/gtfsimport ./cmd/importgtfs \
    && CGO_ENABLED=0 go build -trimpath -o /out/gtfsinspect ./cmd/gtfsinspect

FROM alpine:3.19 AS gtfs-runner
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /work
COPY --from=gtfs-builder /out/gtfsimport /out/gtfsinspect /usr/local/bin/
COPY docker/gtfs-runner.sh /usr/local/bin/gtfs-runner.sh
ENTRYPOINT ["/bin/sh", "/usr/local/bin/gtfs-runner.sh"]
CMD ["gtfsinspect", "--help"]

# Preserve the server as the default target for the existing Compose.
FROM server-runtime AS runtime
