#!/bin/sh
set -eu

: "${AQUAOS_VERSION:?set AQUAOS_VERSION}"
: "${AQUAOS_SIGNING_KEY:?set AQUAOS_SIGNING_KEY to an Ed25519 private PEM file}"
: "${AQUAOS_PUBLIC_KEY_HEX_FILE:?set AQUAOS_PUBLIC_KEY_HEX_FILE to the trusted raw public-key hex file}"

output_dir=${1:-dist}
mkdir -p "$output_dir"
binary="$output_dir/aquaos-linux-amd64"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$binary" ./cmd/aquaos
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$output_dir/aquaosctl-linux-amd64" ./cmd/aquaosctl
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$output_dir/aquaos-admin-linux-amd64" ./cmd/aquaos-admin

for artifact in "$binary" "$output_dir/aquaosctl-linux-amd64" "$output_dir/aquaos-admin-linux-amd64"; do
  digest=$(sha256sum "$artifact" | awk '{print $1}')
  printf '%s  %s\n' "$digest" "$(basename "$artifact")" > "$artifact.sha256"
  printf '%s' "$digest" | openssl pkeyutl -sign -rawin -inkey "$AQUAOS_SIGNING_KEY" > "$artifact.sig"
  od -An -vtx1 "$artifact.sig" | tr -d ' \n' > "$artifact.sig.hex"
done
cp "$AQUAOS_PUBLIC_KEY_HEX_FILE" "$output_dir/aquaos-ed25519-public-key.hex"
