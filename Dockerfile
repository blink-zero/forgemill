# Stage 1: Build frontend
FROM node:20-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
# MED-28: Use npm ci only — fail loudly on lockfile mismatch instead of falling
# back to non-deterministic npm install which could pull tampered packages
RUN npm ci
COPY frontend/ .
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.24-alpine AS go-build
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 9.10: Frontend dist is only needed in the final stage (not embedded in Go binary)
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/forgemill/forgemill/internal/version.Version=${VERSION} -X github.com/forgemill/forgemill/internal/version.Commit=${COMMIT} -X github.com/forgemill/forgemill/internal/version.Date=${BUILD_DATE}" \
    -o /forgemill ./cmd/forgemill

# Stage 3: Final image
FROM alpine:3.20

# Install Packer for Template Factory builds (with checksum verification)
RUN apk add --no-cache ca-certificates tzdata curl unzip xorriso openssl && \
    PACKER_VERSION=$(curl -s https://checkpoint-api.hashicorp.com/v1/check/packer | grep -o '"current_version":"[^"]*"' | cut -d'"' -f4) && \
    curl -fsSL "https://releases.hashicorp.com/packer/${PACKER_VERSION}/packer_${PACKER_VERSION}_SHA256SUMS" -o /tmp/SHA256SUMS && \
    curl -fsSL "https://releases.hashicorp.com/packer/${PACKER_VERSION}/packer_${PACKER_VERSION}_linux_amd64.zip" -o /tmp/packer_${PACKER_VERSION}_linux_amd64.zip && \
    cd /tmp && grep "linux_amd64.zip" SHA256SUMS | sha256sum -c && \
    unzip /tmp/packer_${PACKER_VERSION}_linux_amd64.zip -d /usr/local/bin/ && \
    rm /tmp/packer_${PACKER_VERSION}_linux_amd64.zip /tmp/SHA256SUMS && \
    chmod +x /usr/local/bin/packer && \
    apk del curl unzip
WORKDIR /app
COPY --from=go-build /forgemill /app/forgemill
COPY --from=frontend-build /app/frontend/dist /app/frontend/dist

ENV FORGEMILL_LISTEN_ADDR=:8080
ENV FORGEMILL_DB_PATH=/app/data/forgemill.db
ENV FORGEMILL_FRONTEND_PATH=/app/frontend/dist
ENV FORGEMILL_DATA_DIR=/app/data
ENV PACKER_PLUGIN_PATH=/app/data/packer-plugins
ENV TMPDIR=/app/data/tmp

# Fix 11: Run as non-root user
RUN adduser -D -u 1001 forgemill && \
    mkdir -p /app/data /home/forgemill/.cache /home/forgemill/.config && \
    chown -R forgemill:forgemill /app /home/forgemill
USER forgemill

EXPOSE 8080
VOLUME ["/app/data"]

# 9.3: Docker health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

ENTRYPOINT ["sh", "-c", "mkdir -p /app/data/tmp /app/data/packer-plugins && exec /app/forgemill"]
