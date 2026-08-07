# AquaOS complete installation guide

This guide installs every currently available AquaOS component in a repeatable
way. Follow it in order. Commands identify the machine on which they run.

> **Current safety status:** AquaOS is pre-release. A complete software install
> is suitable for the hardware-incapable simulator and controlled lamp-load
> bench testing only. Do not connect aquarium life-support equipment until the
> physical Prompt 8, clean-VM/recovery, security-CI, and 72-hour soak gates in
> `release-candidate-checklist.md` have passed. AquaOS never replaces an
> independent thermostat, float switch, GFCI/RCD, breaker, UPS, or safe device
> power-return setting.

## 1. What gets installed

Use separate failure domains. Do not install anything directly on the Proxmox
host except Proxmox itself.

| Role | Recommended location | Required for local Core control? | Ports |
|---|---|---:|---|
| AquaOS Core | dedicated minimal Debian Linux amd64 Control VM | yes | 8080/TCP, loopback by default |
| `aquaosctl` | Control VM | recovery/maintenance | none |
| AquaOS Admin GUI | Control VM, started only when needed | no | 8090/TCP loopback |
| Shelly Plug US Gen4 | isolated fish-room LAN/VLAN | only when configured | device HTTP |
| Ethernet/PoE ESP32 node | isolated fish-room LAN/VLAN | only when configured | node HTTP |
| Mosquitto | separate services VM or HAOS broker app | no | 1883 lab; 8883 TLS deployment |
| Home Assistant OS | separate VM | no | 8123/TCP |
| InfluxDB and Grafana | separate services VM | no | 8086/TCP, 3000/TCP |
| Node-RED | advanced add-on only | no | 1880/TCP when explicitly installed |
| `aquaos-vision` | separate optional container host | no | 8091/TCP health |
| Display kiosk | Raspberry Pi 4B | no | outbound browser access only |

Failure of every item marked “no” must leave direct local Core control running.
The physical Proxmox host remains a shared failure domain and can stop the
Control VM.

### Components that are not separate installs

- The event bus, registries, state manager, safety engine, alarm engine, REST
  API, and telemetry pipeline are compiled into the AquaOS Core binary.
- The Admin web assets are embedded in `aquaos-admin`; Node.js or a separate web
  server is not required.
- Reef-Pi is not the Edition 1.2 baseline hardware authority. The repository's
  Reef-Pi adapter is a placeholder and must not be installed as a required
  control-path component.
- The old Telegraf/Reef-Pi MQTT bridge and telemetry publisher remain historical
  development source only and are not part of the active Compose stack.

## 2. Before starting

Prepare:

- A Proxmox host protected by an appropriately sized UPS.
- A DHCP reservation for each VM and LAN device. The examples use names, not
  hardcoded addresses: `aquaos-core`, `aquaos-services`, and `homeassistant`.
- DNS or local resolver entries for those names.
- A management workstation with Git, GNU Make, OpenSSL, and Go 1.25.12.
- A secure offline location for the release-signing private key.
- A non-livestock lamp or other safe resistive bench load if testing hardware.

Record host, VM, device, switch-port, UPS, and VLAN assignments. Back up this
record somewhere outside the Proxmox host.

### Network policy

Allow only the paths needed for the chosen installation:

- Control VM → Shelly and ESP32 HTTP endpoints.
- Control VM → Mosquitto and InfluxDB when those options are enabled.
- Home Assistant → Mosquitto.
- Operator network → Home Assistant and Grafana.
- Operator SSH → Control VM.
- Display Pi → Home Assistant, Grafana, and read-only status pages.

Do not port-forward Core, Admin, Mosquitto, InfluxDB, Grafana, Node-RED, or
device endpoints to the Internet. Keep Admin bound to loopback and reach it
through SSH forwarding.

## 3. Create the Control VM

In the Proxmox web UI create a dedicated Debian 13 or supported Ubuntu Server
amd64 VM. A starting allocation is 2 vCPU, 2 GB RAM, and 16 GB disk. Use a
bridged VirtIO network adapter, QEMU guest agent, a normal virtual disk with
discard enabled where supported, and no USB/device passthrough.

Under the VM **Options**:

1. Enable **Start at boot**.
2. Put the Control VM ahead of noncritical dashboard/AI guests.
3. Set a shutdown timeout long enough for AquaOS and Debian to stop cleanly.

Install only the minimal OS and SSH server. Then run on the Control VM:

```sh
sudo apt update
sudo apt full-upgrade -y
sudo apt install -y ca-certificates curl jq openssh-server qemu-guest-agent unzip
sudo systemctl enable --now qemu-guest-agent ssh
sudo timedatectl set-timezone UTC
timedatectl status
```

Use SSH keys, disable direct root SSH login, and enable the guest firewall.
Permit SSH only from the management network. Do not install Docker on this VM.

Take a clean-OS snapshot for bench recovery, but do not treat a snapshot on the
same host as a backup.

## 4. Build and sign AquaOS

Until official signed release artifacts exist, build from a reviewed commit on
the development workstation:

```sh
git clone https://github.com/tylerkirby004-droid/AquaOS.git
cd AquaOS
git status --short
go version
make test
make vet
make staticcheck
make lint
```

`go version` must report Go 1.25.12 or a later security-patched compatible
release. Generate an Ed25519 key once. Keep the private key offline and never
copy it to the Control VM:

```sh
umask 077
mkdir -p "$PWD/.release-keys"
openssl genpkey -algorithm ED25519 -out "$PWD/.release-keys/aquaos-ed25519.pem"
openssl pkey -in "$PWD/.release-keys/aquaos-ed25519.pem" -pubout -outform DER \
  | tail -c 32 | od -An -vtx1 | tr -d ' \n' \
  > "$PWD/.release-keys/aquaos-ed25519-public.hex"
test "$(wc -c < "$PWD/.release-keys/aquaos-ed25519-public.hex")" -eq 64
```

Build a signed candidate, substituting the intended prerelease version:

```sh
export AQUAOS_VERSION=v1.0.0-rc.1
export AQUAOS_SIGNING_KEY="$PWD/.release-keys/aquaos-ed25519.pem"
export AQUAOS_PUBLIC_KEY_HEX_FILE="$PWD/.release-keys/aquaos-ed25519-public.hex"
scripts/build-signed-release.sh dist
(cd dist && sha256sum -c aquaos-linux-amd64.sha256)
```

Copy `dist/`, `configs/aquaos.yaml`, and the checked-out repository revision to
the Control VM over SSH. Verify the checksum again on the Control VM. Never
install an unsigned or checksum-mismatched binary.

## 5. Install Core in simulator mode

Run on the Control VM from the transferred directory. First perform a dry run:

```sh
chmod 0755 dist/aquaosctl-linux-amd64
sudo ./dist/aquaosctl-linux-amd64 install \
  --binary ./dist/aquaos-linux-amd64 \
  --config ./configs/aquaos.yaml \
  --version v1.0.0-rc.1 \
  --sha256 "$(cut -d' ' -f1 dist/aquaos-linux-amd64.sha256)" \
  --signature ./dist/aquaos-linux-amd64.sig.hex \
  --public-key ./dist/aquaos-ed25519-public-key.hex \
  --ack-control-vm --dry-run
```

Read every reported action. If correct, repeat without `--dry-run`. The
installer refuses non-Linux/amd64 systems and Proxmox hosts, creates the
least-privilege account and directories, installs `/opt/aquaos/bin/aquaos`,
writes the systemd unit, and starts `aquaos.service`.

Install the recovery clients separately because the Core installer deliberately
manages only the authoritative binary:

```sh
sudo install -o root -g root -m 0755 dist/aquaosctl-linux-amd64 /opt/aquaos/bin/aquaosctl
sudo install -o root -g root -m 0755 dist/aquaos-admin-linux-amd64 /opt/aquaos/bin/aquaos-admin
```

Verify:

```sh
sudo /opt/aquaos/bin/aquaosctl status
sudo /opt/aquaos/bin/aquaosctl verify
sudo systemctl --no-pager --full status aquaos.service
sudo journalctl -u aquaos.service -n 100 --no-pager
curl --fail --silent http://127.0.0.1:8080/health/live | jq
curl --fail --silent http://127.0.0.1:8080/health/ready | jq
```

The supplied configuration is intentionally safe: loopback API, simulator on,
MQTT/storage/Home Assistant off, and no hardware adapters.

Test restart and reboot before proceeding:

```sh
sudo systemctl restart aquaos.service
curl --fail --silent http://127.0.0.1:8080/health/ready | jq
sudo reboot
```

After reconnecting, repeat `aquaosctl verify` and the readiness request.

## 6. Test the deterministic simulator

The in-process simulator is a lifecycle-safe placeholder. The deterministic
scenario runner is a separate validation command:

```sh
cd ~/AquaOS
go run ./cmd/aquaos-sim -scenario configs/scenarios/normal-temperature.json
go run ./cmd/aquaos-sim -scenario configs/scenarios/safety-faults.json
go run ./cmd/aquaos-sim -scenario configs/scenarios/adapter-and-integration-faults.json
```

Each command must complete deterministically. Do not confuse these traces with
physical hardware evidence.

## 7. Install optional Mosquitto, InfluxDB, and Grafana

Create a separate Debian services VM. Do not put this stack on the Proxmox host
or make it a Core dependency. Install Docker Engine and the Compose plugin from
Docker's official Debian repository, then verify `docker compose version`.

Copy `infrastructure/docker` to `/opt/aquaos-services` on that VM:

```sh
cd /opt/aquaos-services
cp .env.example .env
chmod 0600 .env
editor .env
```

Replace every placeholder with a unique random value. Create the broker users;
`-c` is used only once because it overwrites the password file:

```sh
docker run --rm -it -v "$PWD/mosquitto/config:/mosquitto/config" \
  eclipse-mosquitto:2 mosquitto_passwd -c /mosquitto/config/passwd aquaos-core
docker run --rm -it -v "$PWD/mosquitto/config:/mosquitto/config" \
  eclipse-mosquitto:2 mosquitto_passwd /mosquitto/config/passwd home-assistant
docker run --rm -it -v "$PWD/mosquitto/config:/mosquitto/config" \
  eclipse-mosquitto:2 mosquitto_passwd /mosquitto/config/passwd aquaos-vision
sudo chown 1883:1883 mosquitto/config/passwd
sudo chmod 0640 mosquitto/config/passwd
```

If the site ID is not `home-reef`, replace `home-reef` in
`mosquitto/config/acl` and later AquaOS configuration with the same lowercase
kebab-case value.

Validate and start:

```sh
docker compose config --quiet
docker compose up -d mosquitto influxdb grafana
docker compose ps
docker compose logs --tail=100 mosquitto influxdb grafana
curl --fail http://127.0.0.1:8086/health
curl --fail http://127.0.0.1:3000/api/health
```

Node-RED is not installed by this standard procedure. Advanced operators can
find its separately isolated add-on instructions in
`../infrastructure/docker/README.md`. It must never contain aquarium control or
safety logic.

### Broker TLS

The included listener is a lab-only authenticated port. Before leaving an
isolated bench network, configure Mosquitto with a private CA, server
certificate, `listener 8883`, `cafile`, `certfile`, and `keyfile`; install the
CA certificate in the Control VM and Home Assistant trust stores; change Core
to `ssl://aquaos-services:8883`; then close port 1883. Do not disable certificate
or hostname verification. Consult Mosquitto's listener/authentication
documentation for the installed major version.

## 8. Enable the Core REST API for LAN clients

The safest default is loopback plus SSH forwarding. If a trusted LAN client
must call REST, create a random token and make it readable only by root and the
`aquaos` group:

```sh
sudo install -d -o root -g aquaos -m 0750 /etc/aquaos/secrets
openssl rand -hex 32 | sudo tee /etc/aquaos/secrets/api.token >/dev/null
sudo chown root:aquaos /etc/aquaos/secrets/api.token
sudo chmod 0640 /etc/aquaos/secrets/api.token
```

Edit a copy of `/etc/aquaos/aquaos.yaml`:

```yaml
http:
  address: "0.0.0.0:8080"
  bearer_token_file: "/etc/aquaos/secrets/api.token"
```

Preserve all other required fields. Validate and activate atomically:

```sh
sudo /opt/aquaos/bin/aquaosctl configure --file ./aquaos-candidate.yaml --dry-run
sudo /opt/aquaos/bin/aquaosctl configure --file ./aquaos-candidate.yaml
```

Permit 8080 only from explicit trusted client addresses. Test with:

```sh
TOKEN=$(sudo cat /etc/aquaos/secrets/api.token)
curl --fail -H "Authorization: Bearer $TOKEN" http://aquaos-core:8080/api/v1/system | jq
```

Do not put tokens in shell history on shared systems. Use a protected client
secret store for normal operation.

## 9. Enable MQTT and Home Assistant discovery

On the Control VM, add the broker username to the candidate YAML but keep its
password out of YAML:

```yaml
mqtt:
  enabled: true
  site_id: home-reef
  broker: "tcp://aquaos-services:1883" # use ssl://...:8883 after TLS setup
  client_id: aquaos-core-1
  username: aquaos-core
  required_for_ready: false
  home_assistant:
    enabled: true
    command_ttl: 10s
    tombstones: []
```

Create a protected systemd environment override rather than writing the broker
password in YAML:

```sh
sudo systemctl edit aquaos.service
```

Enter:

```ini
[Service]
Environment="AQUAOS_MQTT_PASSWORD=REPLACE_WITH_AQUAOS_CORE_PASSWORD"
```

Then run:

```sh
sudo systemctl daemon-reload
sudo /opt/aquaos/bin/aquaosctl configure --file ./aquaos-mqtt.yaml --dry-run
sudo /opt/aquaos/bin/aquaosctl configure --file ./aquaos-mqtt.yaml
sudo journalctl -u aquaos.service -n 100 --no-pager
```

For production operations, replace the plain environment override with your
site's root-readable systemd credential/secrets mechanism. The password must
never enter Git, diagnostics, screenshots, or backups.

Install Home Assistant OS as a separate VM using the official KVM/Proxmox image.
Home Assistant currently recommends at least 2 vCPU and 2 GB RAM; allocate more
for add-ons and history. In Home Assistant:

1. Open **Settings → Devices & services → Add Integration**.
2. Select **MQTT**.
3. Enter the services VM broker name/port and the `home-assistant` credentials.
4. Confirm the MQTT integration connects.
5. Restart AquaOS or reconnect MQTT so retained discovery is reconciled.
6. Verify entities appear under the AquaOS device and availability changes when
   Core stops/starts.

Home Assistant commands publish only to the narrow command topic and are still
validated by AquaOS command and safety policy. If no configured runtime entities
appear, do not create bypass automations: inspect Core inventory and discovery
logs. The current pre-release runtime-inventory mapping is still under release
validation and must be proven for the exact hardware configuration.

Test the optional failure boundary by stopping Mosquitto and Home Assistant.
Core must remain alive and its required local components ready; only optional
integration health may degrade.

## 10. Enable InfluxDB and Grafana

Copy the InfluxDB admin/operator token from the protected services `.env` into a
root-managed file on the Control VM:

```sh
sudo install -o root -g aquaos -m 0640 /dev/null /etc/aquaos/secrets/influx.token
sudo editor /etc/aquaos/secrets/influx.token
```

Prefer a dedicated write-only token for the AquaOS bucket instead of the setup
admin token. Add to the candidate YAML:

```yaml
storage:
  influxdb:
    enabled: true
    url: "http://aquaos-services:8086"
    organization: aquaos
    bucket: telemetry
    token_file: "/etc/aquaos/secrets/influx.token"
    queue_capacity: 4096
    batch_size: 200
    flush_interval: 5s
    retry_minimum: 1s
    retry_maximum: 1m
    write_timeout: 5s
```

Apply through `aquaosctl configure` with a dry run first. Open Grafana at
`http://aquaos-services:3000`, change the initial administrator password, and
open **Dashboards → AquaOS → AquaOS Overview**. The datasource and dashboard
are provisioned from the repository. Empty panels are normal until Core emits
matching state/alarm events.

Stop InfluxDB and confirm Core stays operational. Storage is bounded and
optional; it must never block commands or safety evaluation.

## 11. Start the Admin GUI safely

Admin is a recovery tool, not the daily UI and not a daemon required for Core.
Create a separate long random token:

```sh
openssl rand -hex 32 | sudo tee /etc/aquaos/secrets/admin.token >/dev/null
sudo chown root:aquaos /etc/aquaos/secrets/admin.token
sudo chmod 0640 /etc/aquaos/secrets/admin.token
sudo /opt/aquaos/bin/aquaos-admin \
  -address 127.0.0.1:8090 \
  -token-file /etc/aquaos/secrets/admin.token \
  -authentication-rate 5 -authentication-burst 10 \
  -mutation-rate 2 -mutation-burst 4
```

On the workstation create an SSH tunnel:

```sh
ssh -L 8090:127.0.0.1:8090 operator@aquaos-core
```

Open `http://127.0.0.1:8090/admin/`, paste the token, and test **Status** and
**Verify**. Configuration is validated before activation, and every mutation
calls the same operations service as `aquaosctl`. Stop Admin with Ctrl+C when
finished. Do not expose port 8090 on the LAN.

## 12. Configure Shelly and ESP32 hardware — bench only

There is no safe universal wiring procedure. A qualified person must verify
voltage, current, enclosure, grounding, GFCI/RCD, drip loops, load type, and
device ratings. Use `configs/bench.example.yaml` as a field reference and follow
`prompt-8-bench-checklist.md` exactly.

### Shelly Plug US Gen4

1. Update to the firmware version chosen for the bench and record it.
2. Join the isolated fish-room LAN; block Internet access after provisioning if
   local operation remains functional.
3. Configure its physical power-return behavior to **off**.
4. Give it a reserved address/name and confirm local RPC status with a lamp
   load only.
5. Set `base_url`, channel, unique UUIDs, `safe_on: false`,
   `power_return_policy: off`, finite `maximum_on`, and required probe UUIDs.

### Ethernet/PoE ESP32 dual-probe node

The repository defines the HTTP/JSON contract but does **not** ship production
ESP32 firmware. Install only firmware independently verified to implement
`esp32-node-protocol.md`. Provision a unique bearer token, place the same token
in `/etc/aquaos/secrets/esp32-node.token` with mode 0640/root:aquaos, and verify
node ID, boot ID, monotonic sequence, UTC time, both probe IDs, freshness, and
error behavior.

### Activate the hardware bench

The configuration must set `simulator.enabled: false`, define matching adapter
identifiers (and any declarative inventory used by the site), and finally set
both:

```yaml
bench:
  enabled: true
  safe_load_acknowledged: true
```

These flags acknowledge a controlled bench; they are not production approval.
Apply with a dry run, then observe the full checklist. Any stale probe,
disagreement, adapter loss, stuck relay, restart, or reconciliation failure is a
stop condition. Keep aquarium equipment disconnected.

## 13. Optional `aquaos-vision`

The scaffold ships no trained model and reports degraded/model-unavailable
health until a deployment-specific model is supplied. It is not required for
Core and must use the `aquaos-vision` MQTT account, whose ACL cannot write
command or actuator topics.

On a separate container host:

```sh
cd aquaos-vision
docker build -t aquaos-vision:0.1.0 .
docker run --rm -e AQUAOS_VISION_HEALTH_HOST=0.0.0.0 \
  -p 127.0.0.1:8091:8091 aquaos-vision:0.1.0
```

Expected readiness without a model is HTTP 503 with
`{"ready":false,"state":"model_unavailable"}`. Removing the container must
have zero effect on Core. Model wiring, camera permissions, and MQTT credentials
must remain in this separate deployment unit; never provide actuator access.

## 14. Optional Raspberry Pi display kiosk

Install current Raspberry Pi OS on a Pi 4B, create an unprivileged kiosk user,
apply updates, and configure Chromium kiosk mode to open Home Assistant,
Grafana, or a read-only status page. Give the Pi no Core administrator token,
MQTT write credential, or device-network management access.

Test by unplugging the Pi. Core, hardware polling, alarms, and commands must be
unchanged. If they change, the architecture is incorrect and must not proceed.

## 15. Backup, restore, upgrade, and rollback

Create a protected backup on the Control VM:

```sh
sudo /opt/aquaos/bin/aquaosctl backup --out /root/aquaos-backup.zip
sha256sum /root/aquaos-backup.zip
```

Copy the archive and checksum off the Proxmox host. Test restore on a separate
clean Control VM:

```sh
sudo /opt/aquaos/bin/aquaosctl restore --file ./aquaos-backup.zip --dry-run
sudo /opt/aquaos/bin/aquaosctl restore --file ./aquaos-backup.zip
sudo /opt/aquaos/bin/aquaosctl verify
```

For upgrades, dry-run the signed artifact first:

```sh
sudo /opt/aquaos/bin/aquaosctl upgrade \
  --binary ./aquaos-linux-amd64 --version vNEXT \
  --sha256 "$(cut -d' ' -f1 aquaos-linux-amd64.sha256)" \
  --signature ./aquaos-linux-amd64.sig.hex \
  --public-key ./aquaos-ed25519-public-key.hex --dry-run
```

Repeat without `--dry-run`, verify health and state reconciliation, and use
`aquaosctl rollback --dry-run` followed by `rollback` only if the validated
upgrade runbook requires it.

Back up Home Assistant, InfluxDB, Grafana, and Mosquitto separately. A Core
backup does not contain their data or broker passwords.

## 16. End-to-end acceptance

An installation is functioning only when all applicable checks pass:

- `aquaosctl verify` passes after install, process restart, and VM reboot.
- `/health/live` and `/health/ready` return success for required components.
- Core starts with Mosquitto, Home Assistant, InfluxDB/Grafana, every advanced
  integration add-on, vision, Internet, and display Pi all stopped.
- MQTT ACL negative tests deny Home Assistant and AI actuator/desired-state
  privileges.
- Home Assistant discovery identity remains stable after restart.
- Influx/Grafana outage causes only optional degradation.
- Backup restore succeeds on a replacement Control VM.
- Proxmox VM stop/start and host-maintenance recovery are recorded.
- Hardware fault and lamp-load checklists pass.
- The 72-hour soak shows no unacceptable resource growth.

Record evidence in `release-candidate-checklist.md` and the Prompt 8/12 reports.
Do not convert “not tested” into “pass.” Until every required physical gate is
complete, this remains a simulator/bench installation—not a production aquarium
controller.

## 17. Troubleshooting map

| Symptom | First checks |
|---|---|
| Core will not start | `journalctl -u aquaos`, YAML schema, secret-file ownership, simulator/adapter conflict |
| Readiness is 503 | `/health/ready`, required component state, adapter reachability |
| REST returns 401 | correct bearer token file/client header; token ≥32 characters |
| MQTT disconnected | DNS/port/TLS trust, username/password override, exact site ACL |
| Home Assistant entities absent | MQTT integration, retained discovery, Core inventory/discovery logs |
| Influx panels empty | token scope, org/bucket spelling, Core storage health, matching measurement names |
| Admin is unreachable | process running, SSH tunnel active, listener still loopback |
| Hardware configuration rejected | simulator disabled, both bench guards true, UUID/reference/probe requirements |
| Upgrade fails | checksum/signature/public key/version, then inspect automatic rollback result |

Never “fix” an installation by disabling authentication, widening ACLs, making
optional services authoritative, running Core as root, or bypassing command and
configuration policy.

## 18. Authoritative upstream references

Use these sources for steps owned by external products; their current
instructions take precedence over copied package-manager commands:

- [Proxmox VE administration guide](https://pve.proxmox.com/pve-docs/pve-admin-guide.html)
- [Docker Engine on Debian](https://docs.docker.com/engine/install/debian/)
- [Docker Compose plugin](https://docs.docker.com/compose/install/linux/)
- [Mosquitto authentication](https://mosquitto.org/documentation/authentication-methods/)
- [Home Assistant OS virtual-machine installation](https://www.home-assistant.io/installation/alternative)
- [Home Assistant MQTT integration](https://www.home-assistant.io/integrations/mqtt/)
- [InfluxDB Docker installation](https://docs.influxdata.com/influxdb/v2/install/use-docker-compose/)
- [Grafana Docker installation](https://grafana.com/docs/grafana/latest/setup-grafana/installation/docker/)
- [Raspberry Pi OS installation](https://www.raspberrypi.com/documentation/computers/getting-started.html)
