#!/usr/bin/env bash
set -Eeuo pipefail

readonly alpine_base_url=https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/armv7/netboot-3.24.1
readonly alpine_repository_url=http://dl-cdn.alpinelinux.org/alpine/v3.24/main
readonly artifact_cache_dir=.cache/kobodeck-testvm
readonly readeck_image=codeberg.org/readeck/readeck:0.22.3
readonly admin_user=testadmin
readonly admin_pass=testpass123
readonly admin_email=testadmin@test.invalid
readonly article_url=https://example.com
readonly qemu_host_gateway=10.0.2.2

container_id=
work_dir=

cleanup() {
	if [[ -n "$container_id" ]]; then
		docker stop --time 1 "$container_id" >/dev/null 2>&1 || true
		container_id=
	fi
	if [[ -n "$work_dir" ]]; then
		rm -rf "$work_dir"
		work_dir=
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

die() {
	echo "vm test: $*" >&2
	exit 1
}

for command in cpio curl docker find gzip mkfs.vfat qemu-system-arm sha256sum timeout truncate; do
	command -v "$command" >/dev/null || die "required command not found: $command"
done
[[ -s build/KoboRoot.tgz ]] || die "build/KoboRoot.tgz is missing; run make tarball first"

work_dir=$(mktemp -d)
mkdir -p "$artifact_cache_dir"

download_artifact() {
	local name=$1
	local want_hash=$2
	local path=$artifact_cache_dir/$name
	local got_hash=

	if [[ -f "$path" ]]; then
		got_hash=$(sha256sum "$path")
		got_hash=${got_hash%% *}
		if [[ "$got_hash" == "$want_hash" ]]; then
			printf '%s\n' "$path"
			return
		fi
	fi

	echo "vm test: downloading $name" >&2
	curl --fail --location --silent --show-error \
		--output "$work_dir/$name" "$alpine_base_url/$name"
	got_hash=$(sha256sum "$work_dir/$name")
	got_hash=${got_hash%% *}
	[[ "$got_hash" == "$want_hash" ]] ||
		die "$name sha256 is $got_hash, want $want_hash"
	mv "$work_dir/$name" "$path"
	printf '%s\n' "$path"
}

kernel=$(download_artifact \
	vmlinuz-lts \
	f36e6732b8165eb43780b86a87a3eb2e17fbabc9984bab9796a9a72c4d56debc)
base_initramfs=$(download_artifact \
	initramfs-lts \
	32807f8009a39c86bc70d601e9175293d416a7d204275f807e490cd92653cd68)

echo "vm test: starting Readeck"
container_id=$(docker run --detach --rm \
	--publish 127.0.0.1::8000 \
	"$readeck_image")

healthy=false
for _ in {1..240}; do
	if docker exec "$container_id" readeck healthcheck >/dev/null 2>&1; then
		healthy=true
		break
	fi
	sleep 0.25
done
if [[ "$healthy" != true ]]; then
	docker logs "$container_id" >&2 || true
	die "Readeck did not become healthy"
fi

docker exec "$container_id" readeck user \
	-user "$admin_user" \
	-password "$admin_pass" \
	-email "$admin_email" >/dev/null

port_mapping=$(docker port "$container_id" 8000/tcp)
host_port=${port_mapping##*:}
[[ "$host_port" =~ ^[0-9]+$ ]] || die "cannot parse Readeck port mapping: $port_mapping"
host_url=http://127.0.0.1:$host_port
vm_url=http://$qemu_host_gateway:$host_port

cookie_jar=$work_dir/readeck-cookies
curl --fail --location --silent --show-error \
	--cookie-jar "$cookie_jar" \
	--data-urlencode "username=$admin_user" \
	--data-urlencode "password=$admin_pass" \
	--data-urlencode 'redirect=' \
	"$host_url/login" >/dev/null
token_page=$(curl --fail --location --silent --show-error \
	--cookie "$cookie_jar" --data '' "$host_url/profile/tokens")
token_pattern='value="Authorization: Bearer ([^"]+)"'
if [[ $token_page =~ $token_pattern ]]; then
	token=${BASH_REMATCH[1]}
else
	die "API token not found in Readeck profile page"
fi

bookmark_headers=$work_dir/bookmark-headers
curl --fail --silent --show-error \
	--dump-header "$bookmark_headers" --output /dev/null \
	--header "Authorization: Bearer $token" \
	--header 'Content-Type: application/json' \
	--data "{\"url\":\"$article_url\"}" \
	"$host_url/api/bookmarks"
bookmark_id=$(awk 'tolower($1) == "bookmark-id:" {gsub("\r", "", $2); print $2}' \
	"$bookmark_headers")
[[ -n "$bookmark_id" ]] || die "Readeck response has no Bookmark-Id header"

loaded=false
loaded_pattern='"loaded"[[:space:]]*:[[:space:]]*true'
for _ in {1..120}; do
	bookmark=$(curl --fail --silent --show-error \
		--header "Authorization: Bearer $token" \
		"$host_url/api/bookmarks/$bookmark_id")
	if [[ $bookmark =~ $loaded_pattern ]]; then
		loaded=true
		break
	fi
	sleep 0.25
done
[[ "$loaded" == true ]] || die "Readeck bookmark did not load"

overlay=$work_dir/overlay
mkdir -p "$overlay/etc/apk" "$overlay/test"
cp vm_test_guest.sh "$overlay/vm_test_guest.sh"
cp build/KoboRoot.tgz "$overlay/test/KoboRoot.tgz"
cp testdata/nickel-schema-176.sql "$overlay/test/nickel-schema.sql"
printf '%s\n' "$bookmark_id" >"$overlay/test/bookmark-id"
printf '%s\n' "$alpine_repository_url" >"$overlay/etc/apk/repositories"
cat >"$overlay/test/kobodeck.toml" <<EOF
[Server]
URL = "$vm_url"
Token = "$token"
Timeout = 30

[Fetch]
Workers = 1
Limit = 0
Labels = ""
Status = "unread,reading"

[Sync]
Archive = true
FavouriteCollection = "VM Favourites"

[Log]
Verbose = false
Size = 1

[Output]
Path = "/mnt/onboard/kobodeck"
Delete = false
EOF
chmod 755 "$overlay/vm_test_guest.sh"

overlay_archive=$work_dir/initramfs-overlay.gz
(
	cd "$overlay"
	find . -mindepth 1 -print0 | cpio --null --create --format=newc --quiet | gzip -c
) >"$overlay_archive"
combined_initramfs=$work_dir/initramfs-test
cp "$base_initramfs" "$combined_initramfs"
cat "$overlay_archive" >>"$combined_initramfs"

onboard=$work_dir/onboard.fat
truncate --size 128M "$onboard"
mkfs.vfat "$onboard" >/dev/null

transcript=$work_dir/qemu.log
echo "vm test: booting ARM release smoke test"
if ! timeout 2m qemu-system-arm \
	-machine virt \
	-cpu cortex-a15 \
	-smp 1 \
	-m 256 \
	-display none \
	-monitor none \
	-serial stdio \
	-no-reboot \
	-kernel "$kernel" \
	-initrd "$combined_initramfs" \
	-append 'console=ttyAMA0 rdinit=/vm_test_guest.sh' \
	-netdev user,id=net0 \
	-device virtio-net-device,netdev=net0 \
	-drive "file=$onboard,format=raw,if=none,id=onboard" \
	-device virtio-blk-device,drive=onboard \
	>"$transcript" 2>&1; then
	cat "$transcript" >&2
	die "QEMU failed"
fi
if ! grep -Fq KOBODECK_VM_PASS "$transcript"; then
	cat "$transcript" >&2
	die "guest did not report success"
fi

echo "vm test: PASS"
