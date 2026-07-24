#!/bin/sh
set -u

busybox=/bin/busybox
"$busybox" --install -s
export PATH=/usr/bin:/bin:/usr/sbin:/sbin

fail() {
	echo "KOBODECK_VM_ERROR: $*"
	poweroff -f
	while :; do
		sleep 3600
	done
}

mount -t devtmpfs devtmpfs /dev || fail "mount devtmpfs"
mount -t proc proc /proc || fail "mount proc"
mount -t sysfs sysfs /sys || fail "mount sysfs"
mkdir -p /tmp

modprobe -a virtio_mmio virtio_net af_packet || fail "load network modules"
mdev -s
ip link set lo up || fail "enable loopback"
ip link set eth0 up || fail "enable eth0"
udhcpc -q -n -t 10 -i eth0 -s /usr/share/udhcpc/default.script ||
	fail "obtain DHCP lease"

stty -echo || fail "disable serial echo"
echo "KOBODECK_VM_READY"
/bin/sh -c 'while IFS= read -r command; do eval "$command"; done'

poweroff -f
fail "power off"
