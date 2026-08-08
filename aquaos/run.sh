#!/usr/bin/with-contenv bashio
set -euo pipefail

install -d -m 0750 /etc/aquaos/secrets /var/lib/aquaos/rollback /run/aquaos /etc/systemd/system
if [[ ! -f /config/api.token ]]; then
  api_token="$(head -c 48 /dev/urandom | base64 | tr -d '\n')"
  printf '%s\n' "${api_token}" > /config/api.token
  chmod 0600 /config/api.token
fi
if [[ ! -f /config/aquaos.yaml ]]; then
  site_name="$(bashio::config 'site_name')"
  sed "s/site_id: home-reef/site_id: ${site_name}/" /opt/aquaos/default-aquaos.yaml > /config/aquaos.yaml
fi
ln -sf /config/aquaos.yaml /etc/aquaos/aquaos.yaml
ln -sf /config/api.token /etc/aquaos/secrets/api.token
if [[ -f /config/influxdb.token ]]; then ln -sf /config/influxdb.token /etc/aquaos/secrets/influxdb.token; fi
printf '%s\n' '0.2.0' > /var/lib/aquaos/current-version
printf '%s\n' '[Unit]' 'Description=AquaOS Core (managed by Home Assistant Supervisor)' > /etc/systemd/system/aquaos.service

core_loop() {
  while [[ ! -f /run/aquaos/stopping ]]; do
    /opt/aquaos/bin/aquaos -config /etc/aquaos/aquaos.yaml &
    core_pid=$!
    printf '%s\n' "${core_pid}" > /run/aquaos/core.pid
    wait "${core_pid}" || true
    rm -f /run/aquaos/core.pid
    sleep 1
  done
}

shutdown() {
  touch /run/aquaos/stopping
  if [[ -n "${admin_pid:-}" ]]; then kill "${admin_pid}" 2>/dev/null || true; fi
  if [[ -f /run/aquaos/core.pid ]]; then kill "$(cat /run/aquaos/core.pid)" 2>/dev/null || true; fi
  if [[ -n "${core_loop_pid:-}" ]]; then kill "${core_loop_pid}" 2>/dev/null || true; fi
}
trap shutdown TERM INT EXIT
core_loop &
core_loop_pid=$!

core_ready=false
for _ in $(seq 1 30); do
  if wget -q -O /dev/null --header="Authorization: Bearer $(cat /config/api.token)" http://127.0.0.1:8080/health/ready; then
    core_ready=true
    break
  fi
  sleep 1
done
if [[ "${core_ready}" != true ]]; then
  bashio::log.fatal "AquaOS Core did not become ready; the panel will not start with a partial deployment."
  exit 1
fi

/opt/aquaos/bin/aquaos-admin \
  -address 0.0.0.0:8099 \
  -trusted-ingress \
  -trusted-ingress-cidr 172.30.32.2/32 \
  -home-assistant-websocket ws://supervisor/core/websocket \
  -supervisor-url http://supervisor \
  -history-token-file /config/influxdb.token \
  -history-core-token-file /config/influxdb.token \
  -core-url http://127.0.0.1:8080 \
  -core-token-file /config/api.token &
admin_pid=$!
wait "${admin_pid}"
