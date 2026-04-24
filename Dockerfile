# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.2

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

ARG TARGETOS=linux
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ cmd/
COPY internal/ internal/

RUN --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    goarm=""; \
    if [ "${TARGETARCH}" = "arm" ] && [ -n "${TARGETVARIANT}" ]; then \
      goarm="${TARGETVARIANT#v}"; \
    fi; \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" GOARM="${goarm}" \
      go build \
        -trimpath \
        -ldflags="-s -w -X github.com/soakes/s3ctl/internal/buildinfo.Version=${VERSION} -X github.com/soakes/s3ctl/internal/buildinfo.Commit=${VCS_REF} -X github.com/soakes/s3ctl/internal/buildinfo.BuildDate=${BUILD_DATE}" \
        -o /out/s3ctl \
        ./cmd/s3ctl

FROM debian:bookworm-slim

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="s3ctl" \
      org.opencontainers.image.description="CLI for provisioning S3 buckets and bucket policies" \
      org.opencontainers.image.url="https://github.com/soakes/s3ctl" \
      org.opencontainers.image.source="https://github.com/soakes/s3ctl" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/s3ctl /usr/local/bin/s3ctl

ENTRYPOINT ["/usr/local/bin/s3ctl"]
CMD ["--help"]
