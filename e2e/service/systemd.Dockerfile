# A Debian machine with systemd as PID 1 and a user (u, uid 1000) whose systemd user manager runs at boot
# (lingering), for e2e/service/systemd.sh. Run it with --privileged --cgroupns=host and
# -v /sys/fs/cgroup:/sys/fs/cgroup:rw.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends systemd systemd-sysv dbus dbus-user-session curl ca-certificates procps netcat-openbsd \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -m -u 1000 u \
 && mkdir -p /var/lib/systemd/linger && touch /var/lib/systemd/linger/u
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
