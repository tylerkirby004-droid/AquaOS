#!/bin/sh
set -eu

release=${1:-}
output=${2:-dist/image}
if [ "$(id -u)" -ne 0 ]; then echo "run as root on a disposable Debian build machine" >&2; exit 1; fi
if [ -z "$release" ] || [ ! -d "$release" ]; then echo "usage: $0 SIGNED_RELEASE_DIR [OUTPUT_DIR]" >&2; exit 2; fi
for command in lb go sha256sum; do command -v "$command" >/dev/null 2>&1 || { echo "missing build tool: $command" >&2; exit 1; }; done
: "${AQUAOS_VERSION:?set AQUAOS_VERSION to the signed release version}"
case "$AQUAOS_VERSION" in *[!A-Za-z0-9._+-]*|'') echo "invalid AQUAOS_VERSION" >&2; exit 1;; esac

release=$(readlink -f "$release")
output=$(mkdir -p "$output" && readlink -f "$output")
checksum=$(sed -n '1{s/ .*//;p;}' "$release/aquaos-linux-amd64.sha256")
case "$checksum" in *[!0-9a-f]*|'') echo "invalid release checksum" >&2; exit 1;; esac
if [ "${#checksum}" -ne 64 ]; then echo "release checksum must contain 64 hexadecimal characters" >&2; exit 1; fi
printf '%s  %s\n' "$checksum" "$release/aquaos-linux-amd64" | sha256sum --check --status

work=$(mktemp -d /tmp/aquaos-image.XXXXXX)
cleanup() {
  case "$work" in /tmp/aquaos-image.*) ;; *) echo "refusing unsafe cleanup path" >&2; return;; esac
  (cd "$work" && lb clean --purge >/dev/null 2>&1) || true
  rm -rf -- "$work"
}
trap cleanup EXIT HUP INT TERM
cp -R packaging/appliance-image/config "$work/config"
mkdir -p "$work/config/includes.chroot/usr/share/aquaos-installer" \
  "$work/config/includes.chroot/usr/local/sbin" \
  "$work/config/includes.chroot/etc/systemd/system/multi-user.target.wants"
cp -R "$release"/. "$work/config/includes.chroot/usr/share/aquaos-installer/"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$work/config/includes.chroot/usr/local/sbin/aquaos-firstboot" ./cmd/aquaos-firstboot
sed -e "s/@VERSION@/$AQUAOS_VERSION/g" -e "s/@SHA256@/$checksum/g" \
  packaging/appliance-image/aquaos-firstboot.service > "$work/config/includes.chroot/etc/systemd/system/aquaos-firstboot.service"
ln -s ../aquaos-firstboot.service "$work/config/includes.chroot/etc/systemd/system/multi-user.target.wants/aquaos-firstboot.service"

cd "$work"
lb config --mode debian --distribution trixie --architectures amd64 \
  --binary-images iso-hybrid --debian-installer live --debian-installer-gui true \
  --archive-areas "main non-free-firmware" \
  --bootappend-live "boot=live components hostname=aquaos" \
  --iso-application "AquaOS" --iso-publisher "AquaOS Project" --iso-volume "AQUAOS"
lb build
image="$output/aquaos-$AQUAOS_VERSION-amd64.iso"
cp live-image-amd64.hybrid.iso "$image"
sha256sum "$image" > "$image.sha256"
echo "Created $image"
