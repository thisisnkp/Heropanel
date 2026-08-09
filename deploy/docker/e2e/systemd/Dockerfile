# A container that runs real systemd as PID 1.
#
# Every other e2e image installs `systemctl-shim.sh` as `systemctl`, because
# there is no service manager in a normal container. That shim is enough to prove
# the panel's *logic* — it parses a unit and supervises the process — and it can
# never prove the thing that only systemd decides: whether a unit is valid,
# whether the sandboxing directives on it actually take effect, and whether a
# transient unit really outlives the process that created it.
#
# docs/10 has carried those as honest limits since Phase 0. This image closes
# them without a virtual machine.
#
# It must be run privileged with its own cgroup namespace:
#
#   docker run -d --privileged --cgroupns=private \
#     --tmpfs /tmp --tmpfs /run --tmpfs /run/lock \
#     -v "$PWD/bin/linux-amd64:/np:ro" -v "$PWD/deploy/docker/e2e:/e2e:ro" \
#     nexpanel-systemd
#
# --privileged is what lets systemd manage cgroups and mount its own API
# filesystems. That is acceptable here and nowhere near production: this
# container exists to be a machine for the length of one test run.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
ENV container=docker

RUN apt-get update && apt-get install -y --no-install-recommends \
      systemd systemd-sysv dbus \
      curl ca-certificates procps passwd file libcap2-bin \
    && rm -rf /var/lib/apt/lists/*

# Units that cannot succeed in a container and would otherwise hold the boot in
# "starting" forever, or push it to "degraded" for reasons that have nothing to
# do with what is being tested.
RUN systemctl mask \
      systemd-udevd.service systemd-udevd-kernel.socket systemd-udevd-control.socket \
      systemd-modules-load.service sys-kernel-debug.mount sys-kernel-config.mount \
      systemd-journald-audit.socket systemd-networkd-wait-online.service

# systemd expects SIGRTMIN+3 to mean "shut down cleanly"; without this `docker
# stop` waits the full timeout and then kills PID 1.
STOPSIGNAL SIGRTMIN+3

CMD ["/sbin/init"]
