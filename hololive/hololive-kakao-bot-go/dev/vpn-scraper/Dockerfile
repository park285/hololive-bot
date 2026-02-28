ARG ALPINE_VERSION=3.23
FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    iproute2 \
    iptables \
    dante-server \
    openvpn

COPY entrypoint.sh /entrypoint.sh
COPY sockd.conf /etc/sockd.conf
RUN chmod +x /entrypoint.sh

HEALTHCHECK --interval=60s --timeout=15s --start-period=45s --retries=3 \
    CMD curl -x socks5://127.0.0.1:1080 -s --max-time 10 https://ifconfig.me >/dev/null || exit 1

EXPOSE 1080
ENTRYPOINT ["/entrypoint.sh"]
