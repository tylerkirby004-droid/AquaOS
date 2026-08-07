#!/bin/sh
set -eu

version=${1:?usage: build-candidate.sh VERSION [OUTPUT_DIR]}
output=${2:-dist}
case "$version" in
  v[0-9]*.[0-9]*.[0-9]*-*) ;;
  *) printf '%s\n' 'candidate version must be SemVer with a prerelease suffix' >&2; exit 2 ;;
esac
revision=$(git rev-parse HEAD)
test -z "$(git status --porcelain --untracked-files=no)" || { printf '%s\n' 'tracked worktree must be clean' >&2; exit 2; }
mkdir -p "$output"
for arch in amd64 arm64; do
  for command in aquaos aquaosctl aquaos-admin healthcheck aquaos-sim; do
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -o "$output/$command-linux-$arch" "./cmd/$command"
  done
done
cp configs/aquaos.yaml "$output/aquaos.yaml"
cp packaging/systemd/aquaos.service "$output/aquaos.service"
cp packaging/systemd/aquaos.sysusers "$output/aquaos.sysusers"
cp packaging/systemd/aquaos.tmpfiles "$output/aquaos.tmpfiles"
cp LICENSE "$output/LICENSE"
cp THIRD_PARTY_NOTICES.md "$output/THIRD_PARTY_NOTICES.md"
printf '{"version":"%s","revision":"%s","primary":"linux/amd64","portability":"linux/arm64","status":"candidate"}\n' "$version" "$revision" > "$output/release.json"
(cd "$output" && sha256sum ./* > SHA256SUMS)
printf 'Candidate %s (%s) written to %s\n' "$version" "$revision" "$output"
