# End-to-end test image: a real Ubuntu with OpenLiteSpeed + LSPHP + MariaDB, used
# to verify HeroPanel's broker capabilities against real software (not mocks).
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      wget curl ca-certificates gnupg passwd git file \
      mariadb-server mariadb-client \
    && rm -rf /var/lib/apt/lists/*

# Redis, so an e2e can exercise the REAL deploy path: with a queue configured a
# deploy returns 202 + a job and runs on a worker. The synchronous fallback only
# exists for a panel with no Redis, and a real framework build (npm install +
# next build) takes far longer than an HTTP write timeout allows.
RUN apt-get update && apt-get install -y --no-install-recommends redis-server \
    && rm -rf /var/lib/apt/lists/*

# OpenLiteSpeed from the official repo, plus Ubuntu's php-fpm 8.3 (the vhost
# talks FastCGI to a per-site php-fpm pool).
RUN wget -qO - https://repo.litespeed.sh | bash \
    && apt-get update \
    && apt-get install -y --no-install-recommends openlitespeed \
    && rm -rf /var/lib/apt/lists/*
RUN apt-get update && apt-get install -y --no-install-recommends \
      php8.3-fpm php8.3-mysql php8.3-cli \
    && rm -rf /var/lib/apt/lists/*

# Node.js + npm for the app-runtime e2e (a reverse-proxied Node app, and a real
# Next.js build).
RUN apt-get update && apt-get install -y --no-install-recommends nodejs npm \
    && rm -rf /var/lib/apt/lists/*

# Python + venv for the FastAPI runtime e2e. Ubuntu 24.04 is PEP-668
# ("externally managed"), so a real deploy has to build a venv — which is exactly
# what the site's build command does, and exactly what we want to exercise.
RUN apt-get update && apt-get install -y --no-install-recommends \
      python3 python3-venv python3-pip \
    && rm -rf /var/lib/apt/lists/*

# Go toolchain for the Go runtime e2e (the site's build command compiles a real
# binary as the unprivileged site user).
RUN ARCH="$(dpkg --print-architecture)" \
    && wget -qO /tmp/go.tgz "https://go.dev/dl/go1.23.4.linux-${ARCH}.tar.gz" \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm -f /tmp/go.tgz \
    && ln -sf /usr/local/go/bin/go /usr/local/bin/go \
    && go version

# Pebble: a small ACME (RFC 8555) test CA from the Let's Encrypt team. It lets
# the SSL e2e exercise a *real* HTTP-01 order flow against an actual ACME server
# — account registration, order, challenge validation, finalize, download — as
# opposed to the fake issuer the unit tests use.
RUN GOBIN=/usr/local/bin GOFLAGS=-buildvcs=false \
      go install github.com/letsencrypt/pebble/v2/cmd/pebble@v2.6.0 \
    && test -x /usr/local/bin/pebble

# OpenSSH for the private-repo e2e: the test stands up a real sshd with a real
# bare repo, so the deploy-key path is exercised against actual SSH auth rather
# than a stub. gzip is for database export/import.
RUN apt-get update && apt-get install -y --no-install-recommends \
      openssh-server openssh-client gzip \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /run/sshd

# Composer for the PHP dependency e2e. Installed at the absolute path the broker
# pins (/usr/local/bin/composer) — the deploy must not pick up whatever
# `composer` happens to be first on a site user's PATH.
RUN apt-get update && apt-get install -y --no-install-recommends php8.3-zip unzip zip \
    && rm -rf /var/lib/apt/lists/* \
    && wget -qO /tmp/composer-setup.php https://getcomposer.org/installer \
    && php /tmp/composer-setup.php --install-dir=/usr/local/bin --filename=composer --quiet \
    && rm -f /tmp/composer-setup.php \
    && composer --version

# BIND9 (authoritative) + tools for the DNS e2e. HeroPanel writes zone files into
# /etc/bind/zones and its zone declarations into an include file.
RUN apt-get update && apt-get install -y --no-install-recommends \
      bind9 bind9-utils bind9-dnsutils \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /etc/bind/zones \
    && : > /etc/bind/named.conf.heropanel \
    && printf '\ninclude "/etc/bind/named.conf.heropanel";\n' >> /etc/bind/named.conf.local

# Container shim for systemctl (no systemd here).
COPY systemctl-shim.sh /usr/bin/systemctl
RUN chmod +x /usr/bin/systemctl

# Wire HeroPanel's generated web-server config into OLS's main config. OLS runs
# as its default unprivileged user (nobody); the test script makes the demo site
# docroot world-traversable so OLS can read it (production uses per-vhost suEXEC).
RUN touch /usr/local/lsws/conf/heropanel.conf \
    && printf '\ninclude /usr/local/lsws/conf/heropanel.conf\n' >> /usr/local/lsws/conf/httpd_config.conf

EXPOSE 80 18443
