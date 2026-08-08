# syntax=docker/dockerfile:1
FROM influxdb:2.7 AS influxdb
FROM ghcr.io/hassio-addons/grafana/amd64:12.1.0
ARG BUILD_VERSION
ARG BUILD_ARCH
LABEL io.hass.version="${BUILD_VERSION}" \
      io.hass.type="addon" \
      io.hass.arch="${BUILD_ARCH}" \
      org.opencontainers.image.title="AquaOS Advanced Trends" \
      org.opencontainers.image.source="https://github.com/tylerkirby004-droid/AquaOS"
COPY --from=influxdb /usr/bin/influxd /usr/local/bin/influxd
COPY --from=influxdb /usr/bin/influx /usr/local/bin/influx
COPY aquaos-history/rootfs /
COPY aquaos-history/dashboards /opt/aquaos/dashboards
RUN chmod 0755 /etc/s6-overlay/s6-rc.d/influxdb/run /etc/s6-overlay/s6-rc.d/init-aquaos-history/run
