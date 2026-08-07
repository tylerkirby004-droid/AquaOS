# v0.9 release-candidate acceptance checklist

Every row needs dated evidence, operator, revision, environment, and artifact
digests. `PASS` is forbidden without an attached log or observation. The current
repository is **not release-ready** while any required row is pending.

| Scenario | Method/evidence | Current status |
|---|---|---|
| Unit, race, vet, static and security scans | CI log and `scripts/release-acceptance.sh` | automated suite available |
| API authorization and resource limits | Go tests | PASS at `dcff9fd` locally except race is CI-only |
| MQTT ACL negatives and broker recovery | Go tests plus broker integration log | unit PASS; live broker pending |
| Sensor faults, adapter loss, leak, stuck relay, reconciliation | simulator traces and physical bench log | simulator PASS; hardware pending |
| Home Assistant discovery and broker outage | integration tests plus HA observation | tests PASS; live observation pending |
| Storage and display-Pi outage | optional-outage tests plus operator observation | tests PASS; physical observation pending |
| Backup/restore, upgrade/rollback, headless recovery | clean appliance and replacement-host logs | pending |
| Admin validation and device setup | browser plus CLI recovery test | automated policy tests PASS; end-to-end pending |
| Process restart and appliance reboot | systemd journal and state comparison | pending |
| Power interruption and replacement-machine recovery | timestamps, alarms, recovery state | pending |
| Clean VM install and automatic startup | signed artifact digest and journal | pending |
| Clean dedicated-appliance install | dry-run/apply log, signed artifact digests, browser onboarding | tooling complete; physical run pending |
| Optional-container outage on appliance | `verify-appliance-isolation.sh` evidence | automated tool available; target-host run pending |
| Home Assistant dashboard and local notification | retained MQTT observation, screenshot, notification timestamp | contract tests PASS; live HA pending |
| Replacement-host restore | new-host record and checksum evidence | pending |
| 72-hour lamp-load soak | resource samples, commands, faults, final relay state | pending |

## Soak acceptance bounds

Record RSS, goroutine count, file descriptors, disk use, queue depth, command
latency, reconnect count, and all alarms at least every five minutes. The run
fails for unbounded growth, a missed/unsafe command, unexplained restart, lost
alarm, unreconciled state, or violation of the configured safety policy. Exact
numeric limits must be fixed in the evidence plan before starting the run.

## Release blockers

Prompt 8 lamp-load evidence, Prompt 12 clean/replacement-host evidence, all rows
above, a clean security scan, and the full 72-hour soak are mandatory. No v0.9
or v1 success claim and no v1 tag may be made until they pass.
