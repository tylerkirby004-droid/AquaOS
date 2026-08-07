#!/bin/sh
set -eu

source_dir=${1:-}
target_dir=/opt/aquaos-services
credentials=/root/aquaos-services-credentials.txt

if [ "$(id -u)" -ne 0 ]; then
  echo "run this installer with sudo" >&2
  exit 1
fi
if [ -z "$source_dir" ] || [ ! -f "$source_dir/compose.yaml" ] || [ ! -f "$source_dir/.env.example" ]; then
  echo "a complete optional-services source directory is required" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends docker.io docker-compose openssl ca-certificates
systemctl enable --now docker

install -d -m 0750 "$target_dir"
cp -R "$source_dir/." "$target_dir/"
cd "$target_dir"

if [ ! -f .env ]; then
  influx_password=$(openssl rand -hex 24)
  influx_token=$(openssl rand -hex 32)
  grafana_password=$(openssl rand -hex 24)
  sed \
    -e "s/change-me-influx-password/$influx_password/" \
    -e "s/replace-with-a-long-random-token/$influx_token/" \
    -e "s/change-me-grafana-password/$grafana_password/" \
    .env.example > .env
  chmod 0600 .env
fi

if [ ! -f mosquitto/config/passwd ]; then
  core_password=$(openssl rand -hex 24)
  home_assistant_password=$(openssl rand -hex 24)
  vision_password=$(openssl rand -hex 24)
  docker run --rm -v "$target_dir/mosquitto/config:/mosquitto/config" eclipse-mosquitto:2 \
    mosquitto_passwd -b -c /mosquitto/config/passwd aquaos-core "$core_password"
  docker run --rm -v "$target_dir/mosquitto/config:/mosquitto/config" eclipse-mosquitto:2 \
    mosquitto_passwd -b /mosquitto/config/passwd home-assistant "$home_assistant_password"
  docker run --rm -v "$target_dir/mosquitto/config:/mosquitto/config" eclipse-mosquitto:2 \
    mosquitto_passwd -b /mosquitto/config/passwd aquaos-vision "$vision_password"
  chown 1883:1883 mosquitto/config/passwd
  chmod 0640 mosquitto/config/passwd
  umask 077
  {
    echo "AquaOS optional-services credentials"
    echo "AquaOS Core MQTT password: $core_password"
    echo "Home Assistant MQTT password: $home_assistant_password"
    echo "Vision MQTT password: $vision_password"
    echo "InfluxDB and Grafana credentials: $target_dir/.env"
  } > "$credentials"
fi

docker compose config --quiet
docker compose up -d mosquitto influxdb grafana
docker compose ps
echo "Optional services installed. Root-only credentials: $credentials"
