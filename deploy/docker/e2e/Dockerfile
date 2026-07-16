# End-to-end test image: a real Ubuntu with OpenLiteSpeed + LSPHP + MariaDB, used
# to verify HeroPanel's broker capabilities against real software (not mocks).
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      wget curl ca-certificates gnupg passwd git \
      mariadb-server mariadb-client \
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

# Node.js for the app-runtime e2e (a reverse-proxied Node app).
RUN apt-get update && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

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
