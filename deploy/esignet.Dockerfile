# syntax=docker/dockerfile:1
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# eSignet built from this repository, with the in-repo Thunder engine.
#
# Build context is the repository root (not esignet/esignet-service/ as upstream's Dockerfile
# expects), because esignet-service/go.mod replaces github.com/thunder-id/thunderid with
# ../../thunder/backend. deploy/esignet.Dockerfile.dockerignore keeps the context to those two
# folders.
#
#   docker build -f deploy/esignet.Dockerfile -t ps03-native-sso/esignet:local .
#
# Module and build caches are BuildKit cache mounts, so a rebuild after a small Go change only
# recompiles the changed packages.

ARG GO_IMAGE=golang:1.26-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

FROM ${GO_IMAGE} AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
# Module files first, so the download layer is reused while only source files change.
COPY thunder/backend/go.mod thunder/backend/go.sum thunder/backend/
COPY esignet/esignet-service/go.mod esignet/esignet-service/go.sum esignet/esignet-service/
WORKDIR /src/esignet/esignet-service
RUN --mount=type=cache,id=ps03-gomod,target=/go/pkg/mod \
    go mod download
WORKDIR /src
COPY thunder/backend thunder/backend
COPY esignet/esignet-service esignet/esignet-service
COPY deploy/native-sso-demo.patch /tmp/native-sso-demo.patch
# Demo settings (Native SSO on, Health Card resource server); the shipped deployment.yaml keeps it off.
RUN git apply /tmp/native-sso-demo.patch
WORKDIR /src/esignet/esignet-service
RUN --mount=type=cache,id=ps03-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=ps03-gobuild,target=/root/.cache/go-build \
    mkdir -p /out && go build -trimpath -ldflags="-s -w" -o /out/esignet ./cmd/esignet

FROM ${RUNTIME_IMAGE}
ARG UID=1001
ARG GID=1001
# wget is only for the compose health check.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates wget \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -g ${GID} mosip \
 && useradd -u ${UID} -g mosip -s /usr/sbin/nologin -m mosip \
 && mkdir -p /home/mosip/keystore \
 && chown mosip:mosip /home/mosip/keystore
WORKDIR /home/mosip
COPY --from=build /out/esignet /home/mosip/esignet
COPY --from=build --chown=mosip:mosip /src/esignet/esignet-service/data /home/mosip/data
USER ${UID}:${GID}
ENV DATA_DIR=/home/mosip/data \
    PORT=8080 \
    KEYMANAGER_KEYSTORE_TYPE=PKCS12 \
    KEYMANAGER_PKCS12_FILE_PATH=/home/mosip/keystore/keystore.pfx
EXPOSE 8080
CMD ["/home/mosip/esignet"]
