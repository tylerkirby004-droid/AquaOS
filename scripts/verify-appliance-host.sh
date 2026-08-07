#!/bin/sh
set -eu

test "$(uname -s)" = Linux
test "$(uname -m)" = x86_64
test ! -e /etc/pve
test -x /opt/aquaos/bin/aquaos
test -x /opt/aquaos/bin/aquaosctl
test -f /etc/aquaos/aquaos.yaml
systemctl is-enabled --quiet aquaos.service
systemctl is-active --quiet aquaos.service
curl --fail --silent --show-error http://127.0.0.1:8080/health/live >/dev/null
echo "AquaOS dedicated appliance verification passed"
