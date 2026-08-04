## base
FROM alpine:3.24.1 AS base

RUN apk add --update iproute2-minimal \
    && \
    rm -rf /var/cache/apk/*

ARG DEBUG

WORKDIR /volume

RUN mkdir -p ./bin ./sbin ./lib ./usr/bin ./usr/sbin ./usr/lib ./tmp ./run \
    ./etc/busybox-paths.d/busybox ./etc/logrotate.d ./etc/network/if-up.d \
    ./usr/share/iproute2 ./usr/share/udhcpc ./etc/ssl/misc ./usr/lib/engines-3 ./usr/lib/ossl-modules

RUN    cp -d /lib/ld-musl-* ./lib                                           && echo package musl \
    && cp -d /lib/libc.musl-* ./lib                                         && echo package musl \
    && cp -d /bin/busybox ./bin                                             && echo package busybox \
    && cp -d /etc/busybox-paths.d/busybox ./etc/busybox-paths.d/busybox     && echo package busybox \
    && cp -d /etc/logrotate.d/acpid ./etc/logrotate.d                       && echo package busybox \
    && cp -d /etc/network/if-up.d/dad ./etc/network/if-up.d                 && echo package busybox \
    && cp -d /etc/securetty ./etc                                           && echo package busybox \
    && cp -d /etc/udhcpc/udhcpc.conf ./etc                                  && echo package busybox \
    && cp -d /usr/share/udhcpc/default.script ./usr/share/udhcpc            && echo package busybox \
    && cp -d /bin/sh ./bin                                                  && echo package busybox-binsh \
    && cp -d /sbin/ip ./sbin                                                && echo package iproute2-minimal \
    && cp -d /usr/share/iproute2/* ./usr/share/iproute2                     && echo package iproute2-minimal \
    && cp -d /usr/lib/libelf* ./usr/lib                                     && echo package libelf \
    && cp -d /usr/lib/libmnl.* ./usr/lib                                    && echo package libmnl \
    && cp -d /usr/lib/libcap.* ./usr/lib                                    && echo package libcap2 \
    && cp -d /usr/lib/libpsx.* ./usr/lib                                    && echo package libcap2 \
    && cp -d /usr/lib/libz.* ./usr/lib                                      && echo package zlib \
    && cp -d /usr/lib/libzstd.* ./usr/lib                                   && echo package zstd-libs


RUN if [ "$DEBUG" = "true" ]; then \
       apk add --update net-tools tcpdump ndisc6 iputils-tracepath curl iproute2-tc iproute2-ss iperf3 nmap-nping \
       && cp -d /usr/lib/libpcap* ./usr/lib                                 && echo package tcpdump \
       && cp -d /usr/lib/libssl.so.* ./usr/lib                              && echo package tcpdump \
       && cp -d /usr/lib/libcrypto.so.* ./usr/lib                           && echo package tcpdump \
       && cp -d /usr/lib/libcurl* ./usr/lib                                 && echo package curl \
       && cp -d /usr/lib/libcares* ./usr/lib                                && echo package curl \
       && cp -d /usr/lib/libnghttp* ./usr/lib                               && echo package curl \
       && cp -d /usr/lib/libidn2* ./usr/lib                                 && echo package curl \
       && cp -d /usr/lib/libpsl* ./usr/lib                                  && echo package curl \
       && cp -d /usr/lib/libbrotlidec* ./usr/lib                            && echo package curl \
       && cp -d /usr/lib/libbrotlicommon* ./usr/lib                         && echo package curl \
       && cp -d /usr/lib/libunistring* ./usr/lib                            && echo package curl \
       && cp -d /usr/lib/libiperf* ./usr/lib                                && echo package iperf3 \
       && cp -d /usr/lib/libstdc++* ./usr/lib                               && echo package nmap-nping \
       && cp -d /usr/lib/libgcc* ./usr/lib                                  && echo package nmap-nping \
       && cp -d /usr/lib/libxtables* ./usr/lib                              && echo package iproute2-tc \
       && cp -d /bin/* ./bin && \
       cp -d /usr/bin/* ./usr/bin && \
       cp -d /usr/sbin/* ./usr/sbin && \
       cp -d /sbin/* ./sbin; \
    fi

## gobuilder
FROM --platform=$BUILDPLATFORM golang:1.26.5 AS gobuilder
WORKDIR /build
COPY ./VERSION ./VERSION
COPY ./go.mod ./go.sum ./
RUN go mod download
COPY ./.git ./.git
COPY ./cmd ./cmd
COPY ./pkg ./pkg
COPY ./Makefile ./Makefile
ENV GOCACHE=/root/.cache/go-build

## gobuilder-udp-proxy
FROM gobuilder AS gobuilder-udp-proxy
ARG TARGETARCH
RUN --mount=type=cache,target="/root/.cache/go-build" make build-udp-proxy ARCH=${TARGETARCH}

## udp-proxy
FROM scratch AS udp-proxy
COPY --from=base /volume /
COPY --from=gobuilder-udp-proxy /build/bin/udp-proxy /bin/udp-proxy
ENTRYPOINT ["/bin/udp-proxy"]

## gobuilder-udp-mux
FROM gobuilder AS gobuilder-udp-mux
ARG TARGETARCH
RUN --mount=type=cache,target="/root/.cache/go-build" make build-udp-mux ARCH=${TARGETARCH}

## udp-mux
FROM scratch AS udp-mux
COPY --from=base /volume /
COPY --from=gobuilder-udp-mux /build/bin/udp-mux /bin/udp-mux
ENTRYPOINT ["/bin/udp-mux"]
