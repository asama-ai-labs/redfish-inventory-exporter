# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 Asama Inc.

# Build (must match go.mod / toolchain directive). Base images are pinned by digest.
FROM golang:1.26-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS builder
WORKDIR /app

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

# -s -w: strip symbol table and DWARF (often 25–40% smaller binary)
# -trimpath: remove local paths from the binary
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o redfish-inventory-exporter

# Alpine keeps a shell for debugging; ca-certificates for TLS.
# redfish.yml is not copied; .dockerignore excludes it. Mount it at /data/redfish.yml.
FROM alpine:3.21@sha256:bc21487101ae47172509130ee1092cc273a831968d64b617776d1f69c81e42ab
# /data is the working directory and belongs to the service user. The default config
# (redfish.yml) and log checkpoint file (redfish-log-checkpoints.json) are relative paths, so
# mount the credentials file here, and a volume to keep checkpoints across restarts.
RUN apk add --no-cache ca-certificates \
    && adduser -D redfish-inventory-exporter \
    && mkdir /data && chown redfish-inventory-exporter /data
WORKDIR /data
COPY --from=builder /app/redfish-inventory-exporter /usr/local/bin/redfish-inventory-exporter
USER redfish-inventory-exporter
EXPOSE 9611
ENTRYPOINT ["redfish-inventory-exporter"]
