# syntax=docker/dockerfile:1
# Build on the builder's native platform. TARGETARCH comes from buildx, so an
# arm64 image is cross-compiled without QEMU and without a Go toolchain on the
# target STB, which only loads the small runtime layer.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go/go.mod go/go.sum ./
RUN go mod download
COPY go/ ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/digitrald ./cmd/digitrald

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/digitrald /digitrald
COPY frontend/ /frontend/

ENV FRONTEND_HOME=/app/data \
    FRONTEND_DIR=/frontend \
    PORT=8765 \
    POLL_SEC=60 \
    TZ=Asia/Jakarta

EXPOSE 8765
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/digitrald", "-healthcheck"]
ENTRYPOINT ["/digitrald"]
