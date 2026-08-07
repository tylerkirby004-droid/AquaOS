# syntax=docker/dockerfile:1
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aquaos ./cmd/aquaos && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/aquaos /usr/local/bin/aquaos
COPY --from=build /out/healthcheck /usr/local/bin/healthcheck
COPY configs/aquaos.yaml /etc/aquaos/aquaos.yaml
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/aquaos"]
CMD ["-config", "/etc/aquaos/aquaos.yaml"]
