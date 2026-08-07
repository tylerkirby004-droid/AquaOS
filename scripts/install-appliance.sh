#!/bin/sh
set -eu

usage() {
  echo "usage: $0 --release DIR --repository DIR --version VERSION --sha256 HEX --site-id ID --address LAN_ADDRESS [--timezone ZONE] [--apply --ack-dedicated-appliance --ack-independent-safeguards]" >&2
}

release=. repository=. version= checksum= site_id= address= timezone=UTC
apply=false
ack_appliance=false
ack_safeguards=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --release) release=$2; shift 2 ;;
    --repository) repository=$2; shift 2 ;;
    --version) version=$2; shift 2 ;;
    --sha256) checksum=$2; shift 2 ;;
    --site-id) site_id=$2; shift 2 ;;
    --address) address=$2; shift 2 ;;
    --timezone) timezone=$2; shift 2 ;;
    --apply) apply=true; shift ;;
    --ack-dedicated-appliance) ack_appliance=true; shift ;;
    --ack-independent-safeguards) ack_safeguards=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then echo "run with sudo on a dedicated Debian computer" >&2; exit 1; fi
if [ -d /etc/pve ]; then echo "refusing to install on a Proxmox host" >&2; exit 1; fi
if [ ! -r /etc/os-release ]; then echo "Debian operating-system information is unavailable" >&2; exit 1; fi
. /etc/os-release
if [ "${ID:-}" != debian ] || [ "$(dpkg --print-architecture)" != amd64 ]; then
  echo "the appliance profile requires dedicated Debian amd64" >&2
  exit 1
fi
for value in "$release" "$repository" "$version" "$checksum" "$site_id" "$address"; do
  if [ -z "$value" ]; then usage; exit 2; fi
done
case "$site_id" in *[!a-z0-9-]*|'') echo "invalid site ID" >&2; exit 1;; esac
case "$version$checksum$address$timezone" in *[!A-Za-z0-9._:/+-]*) echo "an argument contains unsupported characters" >&2; exit 1;; esac
if [ "${#checksum}" -ne 64 ]; then echo "SHA-256 must contain 64 hexadecimal characters" >&2; exit 1; fi
case "$checksum" in *[!0-9a-f]*) echo "SHA-256 must be lowercase hexadecimal" >&2; exit 1;; esac
case "$site_id" in [a-z]*) ;; *) echo "site ID must start with a lowercase letter" >&2; exit 1;; esac
if [ "${#site_id}" -gt 63 ]; then echo "site ID is too long" >&2; exit 1; fi
case "$address" in 10.*|192.168.*|172.1[6-9].*|172.2[0-9].*|172.3[01].*) ;; *) echo "address must be a private IPv4 LAN address" >&2; exit 1;; esac

for path in \
  "$release/aquaos-linux-amd64" "$release/aquaosctl-linux-amd64" \
  "$release/aquaos-admin-linux-amd64" "$release/aquaos-ha-config-linux-amd64" \
  "$release/aquaos-admin-linux-amd64.sha256" "$release/aquaos-admin-linux-amd64.sig.hex" \
  "$release/aquaos-ha-config-linux-amd64.sha256" "$release/aquaos-ha-config-linux-amd64.sig.hex" \
  "$release/aquaos-linux-amd64.sig.hex" "$release/aquaos-ed25519-public-key.hex" \
  "$repository/configs/aquaos-appliance.yaml" \
  "$repository/infrastructure/docker/compose.yaml" \
  "$repository/infrastructure/docker/compose.appliance.yaml" \
  "$repository/scripts/install-optional-services.sh" \
  "$repository/homeassistant/appliance/configuration.yaml" \
  "$repository/homeassistant/appliance/automations.yaml"; do
  if [ ! -f "$path" ]; then echo "required installation file is missing: $path" >&2; exit 1; fi
done
printf '%s  %s\n' "$checksum" "$release/aquaos-linux-amd64" | sha256sum --check --status

echo "AquaOS dedicated-appliance plan"
echo "  Native critical service: AquaOS Core under systemd"
echo "  Optional containers: Home Assistant, Mosquitto, InfluxDB, Grafana"
echo "  Browser setup: https://$address:8090/admin/"
echo "  Daily dashboard: http://$address:8123"
echo "  Trends: http://$address:3000"
echo "No aquarium equipment will be commissioned or activated by this installer."
if [ "$apply" != true ]; then
  echo "Dry run only. Repeat with --apply and both acknowledgement flags after review."
  exit 0
fi
if [ "$ack_appliance" != true ] || [ "$ack_safeguards" != true ]; then
  echo "apply requires acknowledgement of the dedicated host and independent physical safeguards" >&2
  exit 2
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends docker.io docker-compose openssl ca-certificates
systemctl enable --now docker
"$repository/scripts/install-optional-services.sh" "$repository/infrastructure/docker" "$site_id"

install -d -m 0750 /etc/aquaos /etc/aquaos/secrets /opt/aquaos/bin /opt/aquaos-homeassistant
printf 'u aquaos - "AquaOS service" /var/lib/aquaos /usr/sbin/nologin\n' > /usr/lib/sysusers.d/aquaos.conf
systemd-sysusers /usr/lib/sysusers.d/aquaos.conf
core_password=$(sed -n 's/^AquaOS Core MQTT password: //p' /root/aquaos-services-credentials.txt)
influx_token=$(sed -n 's/^INFLUXDB_ADMIN_TOKEN=//p' /opt/aquaos-services/.env)
if [ -z "$core_password" ] || [ -z "$influx_token" ]; then echo "generated service credentials are incomplete" >&2; exit 1; fi
printf '%s\n' "$influx_token" > /etc/aquaos/secrets/influx.token
api_token=$(openssl rand -hex 32)
admin_token=$(openssl rand -hex 32)
printf '%s\n' "$api_token" > /etc/aquaos/secrets/api.token
printf 'AQUAOS_MQTT_USERNAME=aquaos-core\nAQUAOS_MQTT_PASSWORD=%s\n' "$core_password" > /etc/aquaos/aquaos.env
chmod 0640 /etc/aquaos/aquaos.env /etc/aquaos/secrets/influx.token /etc/aquaos/secrets/api.token
chown root:aquaos /etc/aquaos/aquaos.env /etc/aquaos/secrets/influx.token /etc/aquaos/secrets/api.token

sed "s/site_id: home-reef/site_id: $site_id/" "$repository/configs/aquaos-appliance.yaml" > /tmp/aquaos-appliance.yaml
chmod 0755 "$release/aquaosctl-linux-amd64" "$release/aquaos-ha-config-linux-amd64"
for artifact in aquaos-admin-linux-amd64 aquaos-ha-config-linux-amd64; do
  artifact_checksum=$(sed -n '1{s/ .*//;p;}' "$release/$artifact.sha256")
  "$release/aquaosctl-linux-amd64" verify-artifact --binary "$release/$artifact" --sha256 "$artifact_checksum" --signature "$release/$artifact.sig.hex" --public-key "$release/aquaos-ed25519-public-key.hex"
done
"$release/aquaosctl-linux-amd64" install --binary "$release/aquaos-linux-amd64" --config /tmp/aquaos-appliance.yaml --version "$version" --sha256 "$checksum" --signature "$release/aquaos-linux-amd64.sig.hex" --public-key "$release/aquaos-ed25519-public-key.hex" --ack-control-vm --dry-run
"$release/aquaosctl-linux-amd64" install --binary "$release/aquaos-linux-amd64" --config /tmp/aquaos-appliance.yaml --version "$version" --sha256 "$checksum" --signature "$release/aquaos-linux-amd64.sig.hex" --public-key "$release/aquaos-ed25519-public-key.hex" --ack-control-vm
install -m 0755 "$release/aquaosctl-linux-amd64" /opt/aquaos/bin/aquaosctl
install -m 0755 "$release/aquaos-admin-linux-amd64" /opt/aquaos/bin/aquaos-admin
install -m 0755 "$release/aquaos-ha-config-linux-amd64" /opt/aquaos/bin/aquaos-ha-config

cp "$repository/homeassistant/appliance/configuration.yaml" /opt/aquaos-homeassistant/configuration.yaml
alarm_entity=$(printf '%s' "aquaos_${site_id}_notification" | tr '-' '_')
sed "s/AQUAOS_ALARM_ENTITY/$alarm_entity/g" "$repository/homeassistant/appliance/automations.yaml" > /opt/aquaos-homeassistant/automations.yaml
"$release/aquaos-ha-config-linux-amd64" --config /etc/aquaos/aquaos.yaml --grafana-url "http://$address:3000" --output /opt/aquaos-homeassistant/aquaos-dashboard.yaml
printf 'TZ=%s\n' "$timezone" >> /opt/aquaos-services/.env
cd /opt/aquaos-services
if docker compose version >/dev/null 2>&1; then
  docker compose -f compose.yaml -f compose.appliance.yaml up -d
else
  docker-compose -f compose.yaml -f compose.appliance.yaml up -d
fi

openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 825 \
  -subj "/CN=$address" -keyout /etc/aquaos/secrets/admin.key \
  -out /etc/aquaos/secrets/admin.crt
printf '%s\n' "$admin_token" > /etc/aquaos/secrets/admin.token
chmod 0600 /etc/aquaos/secrets/admin.key
chmod 0640 /etc/aquaos/secrets/admin.crt /etc/aquaos/secrets/admin.token
cat > /etc/systemd/system/aquaos-admin.service <<EOF
[Unit]
Description=AquaOS guided administration
After=network-online.target aquaos.service
Wants=network-online.target
[Service]
Type=simple
ExecStart=/opt/aquaos/bin/aquaos-admin -address $address:8090 -token-file /etc/aquaos/secrets/admin.token -tls-cert /etc/aquaos/secrets/admin.crt -tls-key /etc/aquaos/secrets/admin.key -core-url http://localhost:8080 -core-token-file /etc/aquaos/secrets/api.token
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/aquaos-homeassistant-dashboard.service <<EOF
[Unit]
Description=Regenerate the non-authoritative AquaOS Home Assistant dashboard
[Service]
Type=oneshot
ExecStart=/opt/aquaos/bin/aquaos-ha-config --config /etc/aquaos/aquaos.yaml --grafana-url http://$address:3000 --output /opt/aquaos-homeassistant/aquaos-dashboard.yaml
EOF
cat > /etc/systemd/system/aquaos-homeassistant-dashboard.path <<EOF
[Unit]
Description=Watch AquaOS configuration for Home Assistant dashboard changes
[Path]
PathChanged=/etc/aquaos/aquaos.yaml
Unit=aquaos-homeassistant-dashboard.service
[Install]
WantedBy=multi-user.target
EOF
install -d -m 0755 /etc/systemd/system/docker.service.d
cat > /etc/systemd/system/docker.service.d/aquaos-resource-priority.conf <<EOF
[Service]
CPUWeight=100
OOMScoreAdjust=500
EOF
systemctl daemon-reload
systemctl enable --now aquaos-admin.service aquaos-homeassistant-dashboard.path
systemctl restart aquaos.service docker.service
/opt/aquaos/bin/aquaosctl verify

umask 077
{
  echo "AquaOS Admin access code: $admin_token"
  echo "AquaOS Core API token: $api_token"
  echo "Other service credentials: /root/aquaos-services-credentials.txt"
} > /root/aquaos-appliance-credentials.txt
echo "Installation complete. Open https://$address:8090/admin/ and use the access code in /root/aquaos-appliance-credentials.txt"
echo "Home Assistant onboarding is at http://$address:8123; a browser owner account must still be created."
