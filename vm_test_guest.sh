#!/bin/sh
set -u

busybox=/bin/busybox
log=/mnt/onboard/.adds/kobodeck/kobodeck.log
nickel_status=/tmp/nickel-hardware-status
binary=/usr/local/bin/kobodeck
rule=/etc/udev/rules.d/90-kobodeck.rules

echo "KOBODECK_VM: installing BusyBox applets"
"$busybox" --install -s
export PATH=/usr/bin:/bin:/usr/sbin:/sbin

fail() {
	echo "KOBODECK_VM_ERROR: $*"
	poweroff -f
	while :; do
		sleep 3600
	done
}

require_log() {
	grep -Fq "$1" "$log" || fail "Kobodeck log is missing: $1"
}

echo "KOBODECK_VM: mounting virtual filesystems"
mount -t devtmpfs devtmpfs /dev || fail "mount devtmpfs"
mount -t proc proc /proc || fail "mount proc"
mount -t sysfs sysfs /sys || fail "mount sysfs"
mkdir -p /tmp

echo "KOBODECK_VM: configuring devices and network"
modprobe -a virtio_mmio virtio_net virtio_blk af_packet fat vfat nls_cp437 nls_iso8859_1 ||
	fail "load kernel modules"
mdev -s
ip link set lo up || fail "enable loopback"
ip link set eth0 up || fail "enable eth0"
udhcpc -q -n -t 10 -i eth0 -s /usr/share/udhcpc/default.script ||
	fail "obtain DHCP lease"
mkdir -p /mnt/onboard
mount -t vfat /dev/vda /mnt/onboard || fail "mount onboard FAT storage"
mount | grep -q ' on /mnt/onboard type vfat ' || fail "verify onboard FAT storage"

echo "KOBODECK_VM: installing test runtime and release"
apk add --initdb eudev sqlite || fail "install eudev and sqlite"
tar -xzf /test/KoboRoot.tgz -C / || fail "install KoboRoot.tgz"
mkdir -p /mnt/onboard/.adds/kobodeck /mnt/onboard/.kobo
cp /test/kobodeck.toml /mnt/onboard/.adds/kobodeck/kobodeck.toml ||
	fail "install Kobodeck configuration"
sqlite3 /mnt/onboard/.kobo/KoboReader.sqlite </test/nickel-schema.sql ||
	fail "create Nickel database"
touch "$nickel_status" || fail "create Nickel status file"

echo "KOBODECK_VM: starting udev with a wlan interface"
ip link set eth0 down || fail "disable eth0"
ip link set eth0 name wlan0 || fail "rename network interface"
ip link set wlan0 up || fail "enable wlan0"
mkdir -p /run/udev
udevd --daemon || fail "start udev"
udevadm control --reload-rules || fail "reload udev rules"
[ ! -e "$log" ] || fail "Kobodeck ran before a Wi-Fi event"

echo "KOBODECK_VM: verifying remove event is ignored"
udevadm trigger --action=remove /sys/class/net/wlan0 || fail "trigger remove event"
sleep 1
[ ! -e "$log" ] || fail "remove event unexpectedly ran Kobodeck"

echo "KOBODECK_VM: triggering release through installed udev rule"
udevadm trigger --action=add /sys/class/net/wlan0 || fail "trigger add event"
waited=0
while ! grep -q ' completed in ' "$log" 2>/dev/null; do
	waited=$((waited + 1))
	[ "$waited" -lt 60 ] || fail "Kobodeck did not complete"
	sleep 1
done

bookmark_id=$(cat /test/bookmark-id)
download=/mnt/onboard/kobodeck/$bookmark_id.kepub.epub
[ -s "$download" ] || fail "downloaded KEPUB is missing"
if grep -Eiq 'error|failed|panic' "$log"; then
	fail "Kobodeck log contains an error"
fi
for fragment in \
	'pid=' \
	'action="add"' \
	'interface="wlan0"' \
	'connecting to ' \
	'downloading ' \
	'converted to ' \
	'triggering Nickel rescan' \
	'completed in '; do
	require_log "$fragment"
done
grep -Fq 'usb plug add' "$nickel_status" || fail "Nickel add event is missing"
grep -Fq 'usb plug remove' "$nickel_status" || fail "Nickel remove event is missing"

echo "KOBODECK_VM: verifying release uninstall through udev"
: >/mnt/onboard/.adds/kobodeck/kobodeck.toml
udevadm trigger --action=add /sys/class/net/wlan0 || fail "trigger uninstall event"
waited=0
while [ -e "$binary" ] || [ -e "$rule" ] || [ -e /mnt/onboard/.adds/kobodeck ]; do
	waited=$((waited + 1))
	[ "$waited" -lt 30 ] || fail "Kobodeck did not uninstall"
	sleep 1
done
[ -s "$download" ] || fail "uninstall removed downloaded article"

sync
echo "KOBODECK_VM_PASS"
poweroff -f
fail "power off"
