#!/bin/sh
set -eu

evidence=${1:-}
ack=${2:-}
if [ "$(id -u)" -ne 0 ]; then echo "run with sudo on the AquaOS appliance" >&2; exit 1; fi
if [ -z "$evidence" ] || [ "$ack" != "--ack-stop-optional-services" ]; then
  echo "usage: $0 EVIDENCE_FILE --ack-stop-optional-services" >&2
  exit 2
fi
case "$evidence" in /*) ;; *) echo "evidence file must be an absolute path" >&2; exit 1;; esac
if ! systemctl is-active --quiet aquaos.service; then echo "AquaOS Core is not active before the test" >&2; exit 1; fi

restore_optional() { systemctl start docker.service >/dev/null 2>&1 || true; }
trap restore_optional EXIT HUP INT TERM
{
  echo "AquaOS optional-service isolation test"
  TZ=UTC0 date '+started=%Y-%m-%dT%H:%M:%SZ'
  systemctl stop docker.service
  echo "docker=stopped"
  systemctl is-active aquaos.service
  curl --fail --silent --show-error http://localhost:8080/health/live
  curl --fail --silent --show-error http://localhost:8080/health/ready
  /opt/aquaos/bin/aquaosctl verify
  echo "result=PASS"
  TZ=UTC0 date '+finished=%Y-%m-%dT%H:%M:%SZ'
} > "$evidence" 2>&1
restore_optional
trap - EXIT HUP INT TERM
echo "Isolation check passed; evidence written to $evidence"
