ARG MTPLATFORM=linux/amd64

FROM --platform=$MTPLATFORM debian:bookworm-slim AS builder

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        git \
        libssl-dev \
        zlib1g-dev; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY . .
RUN make -j"$(nproc)"

FROM --platform=$MTPLATFORM debian:bookworm-slim

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        iproute2; \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/objs/bin/mtproto-proxy /usr/local/bin/mtproto-proxy
COPY docker/telegram/ /etc/telegram/
COPY docker/run.sh /run.sh

RUN set -eux; \
    mkdir -p /data; \
    chmod 0755 /run.sh; \
    if [ -f /etc/telegram/hello-explorers-how-are-you-doing ]; then chmod 0600 /etc/telegram/hello-explorers-how-are-you-doing; fi

CMD ["/bin/sh", "-c", "/bin/bash /run.sh"]
