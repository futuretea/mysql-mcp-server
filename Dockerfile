# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w \
      -X example.invalid/mcp-template-module-placeholder/pkg/core/version.Version=${VERSION} \
      -X example.invalid/mcp-template-module-placeholder/pkg/core/version.Commit=${GIT_COMMIT} \
      -X example.invalid/mcp-template-module-placeholder/pkg/core/version.Date=${BUILD_DATE}" \
    -o /usr/local/bin/mcp-template-binary-placeholder ./cmd/mcp-server

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S mcp \
    && adduser -S -G mcp mcp

USER mcp

ENTRYPOINT ["/usr/local/bin/mcp-template-binary-placeholder", "mcp"]

FROM runtime AS dev

COPY --from=builder /usr/local/bin/mcp-template-binary-placeholder /usr/local/bin/mcp-template-binary-placeholder
