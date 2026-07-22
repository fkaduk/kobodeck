ARG ALPINE_VERSION=3.24.1
FROM --platform=linux/arm/v7 alpine:${ALPINE_VERSION}

RUN apk add --no-cache \
        alpine-base \
        ca-certificates \
        eudev \
        iproute2 \
        linux-virt \
        openssh \
    && ssh-keygen -A \
    && mkdir -p /root/.ssh /mnt/onboard /run/udev \
    && chmod 0700 /root/.ssh

COPY init /sbin/kobodeck-test-init
COPY sshd_config /etc/ssh/sshd_config
RUN chmod 0755 /sbin/kobodeck-test-init
