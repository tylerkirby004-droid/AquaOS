# syntax=docker/dockerfile:1
ARG BUILD_FROM
FROM golang:1.25.12-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/aquaos ./cmd/aquaos && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/aquaos-admin ./cmd/aquaos-admin

FROM ${BUILD_FROM}
COPY --from=build /out/aquaos /opt/aquaos/bin/aquaos
COPY --from=build /out/aquaos-admin /opt/aquaos/bin/aquaos-admin
COPY aquaos/run.sh /run.sh
COPY aquaos/systemctl /usr/local/bin/systemctl
COPY aquaos/default-aquaos.yaml /opt/aquaos/default-aquaos.yaml
RUN chmod 0755 /run.sh /usr/local/bin/systemctl /opt/aquaos/bin/aquaos /opt/aquaos/bin/aquaos-admin
CMD ["/run.sh"]
