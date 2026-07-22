#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 OUTPUT_DIR SSH_PUBLIC_KEY" >&2
	exit 2
fi

output_dir=$1
public_key=$2
image_tag=kobodeck-testvm-rootfs:alpine-3.24.1-armv7
cache_dir=${KOBODECK_VM_CACHE_DIR:-}

for command in docker mkfs.ext4 mkfs.vfat truncate; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "missing required command: $command" >&2
		exit 1
	fi
done

mkdir -p "$output_dir"
rootfs_dir=$(mktemp -d)
container_id=
cleanup() {
	if [ -n "$container_id" ]; then
		docker rm -f "$container_id" >/dev/null 2>&1 || true
	fi
	rm -rf "$rootfs_dir"
}
trap cleanup EXIT INT TERM

if [ -n "$cache_dir" ] && [ -f "$cache_dir/rootfs-image.tar" ]; then
	docker load --input "$cache_dir/rootfs-image.tar" >/dev/null
fi
docker build --platform linux/arm/v7 --tag "$image_tag" testvm
if [ -n "$cache_dir" ]; then
	mkdir -p "$cache_dir"
	docker save --output "$cache_dir/rootfs-image.tar" "$image_tag"
fi
container_id=$(docker create "$image_tag")
docker export "$container_id" | tar --no-same-owner -xf - -C "$rootfs_dir"

# mkfs.ext4 populates the image as the host user and must be able to read every
# staged file. Alpine contains execute-only helpers such as /bin/bbsuid.
find "$rootfs_dir" -type d -exec chmod u+rx {} +
find "$rootfs_dir" -type f -exec chmod u+r {} +

mkdir -p "$rootfs_dir/root/.ssh"
cp "$public_key" "$rootfs_dir/root/.ssh/authorized_keys"
chmod 0600 "$rootfs_dir/root/.ssh/authorized_keys"

kernel=$(find "$rootfs_dir/boot" -maxdepth 1 -name 'vmlinuz-virt' -print -quit)
initramfs=$(find "$rootfs_dir/boot" -maxdepth 1 -name 'initramfs-virt' -print -quit)
if [ -z "$kernel" ] || [ -z "$initramfs" ]; then
	echo "Alpine linux-virt did not provide a kernel and initramfs" >&2
	exit 1
fi
cp "$kernel" "$output_dir/vmlinuz-virt"
cp "$initramfs" "$output_dir/initramfs-virt"

truncate -s 512M "$output_dir/root.ext4"
mkfs.ext4 -q -F -d "$rootfs_dir" "$output_dir/root.ext4"

truncate -s 64M "$output_dir/onboard.fat"
mkfs.vfat "$output_dir/onboard.fat" >/dev/null
