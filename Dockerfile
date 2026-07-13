# Stage 1: Build React SPA
FROM node:22-alpine AS ui-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Stage 2: Build Go binaries (hpd, hp-broker, hpctl)
FROM golang:1.26-bookworm AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
# Copy built UI from stage 1 into web/dist so it embeds into hpd
COPY --from=ui-builder /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/hpd ./cmd/hpd && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/hp-broker ./cmd/hp-broker && \
    (CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/hp-installer ./cmd/hp-installer || true)

# Stage 3: Production/Runtime Ubuntu image with OpenLiteSpeed + MariaDB + PHP
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget gnupg passwd \
      mariadb-server mariadb-client \
    && rm -rf /var/lib/apt/lists/*

# Install OpenLiteSpeed & PHP 8.3
RUN wget -qO - https://repo.litespeed.sh | bash \
    && apt-get update \
    && apt-get install -y --no-install-recommends openlitespeed \
    && rm -rf /var/lib/apt/lists/*
RUN apt-get update && apt-get install -y --no-install-recommends \
      php8.3-fpm php8.3-mysql php8.3-cli \
    && rm -rf /var/lib/apt/lists/*

# Container shim for systemctl (when OLS or modules invoke systemctl)
COPY deploy/docker/e2e/systemctl-shim.sh /usr/bin/systemctl
RUN chmod +x /usr/bin/systemctl

# Wire HeroPanel's generated web-server config into OLS main config
RUN touch /usr/local/lsws/conf/heropanel.conf \
    && printf '\ninclude /usr/local/lsws/conf/heropanel.conf\n' >> /usr/local/lsws/conf/httpd_config.conf

# Copy HeroPanel binaries from go-builder
COPY --from=go-builder /out/* /usr/local/bin/

# Prepare directories and entrypoint script
RUN mkdir -p /run/heropanel /srv/heropanel/sites /srv/heropanel/data /run/mysqld \
    && chown mysql:mysql /run/mysqld

COPY deploy/docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 80 443 18443

ENTRYPOINT ["/entrypoint.sh"]
