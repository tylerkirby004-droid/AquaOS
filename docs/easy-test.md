# Quick, safe test

Do not connect live equipment. The easiest hardware-incapable test is:

```sh
make bootstrap
make simulate
```

For a release-shaped Linux test:

```sh
scripts/build-candidate.sh v1.0.0-rc.1 dist
sha256sum -c dist/SHA256SUMS
dist/aquaos-linux-amd64 -config dist/aquaos.yaml
```

In another terminal run `dist/healthcheck-linux-amd64 -url
http://127.0.0.1:8080/health/ready`. Candidate artifacts are not production
approval and are intentionally not committed.
