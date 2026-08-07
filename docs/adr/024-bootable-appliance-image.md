# ADR-024: Distribute a bootable appliance installer image

## Status

Accepted

## Context

The signed command-line installer still required a new operator to install
Debian, use SSH, locate a checksum, and enter a long command. Those steps made
safe installation harder without improving the runtime authority boundary.

## Decision

Publish a signed/checksummed Debian amd64 hybrid ISO built reproducibly with
Debian `live-build`. The USB boots the official Debian Installer and leaves disk
selection and destructive partition confirmation visible to the operator. The
installed system starts a temporary authenticated first-boot page at
`https://aquaos.local:8443`.

Each machine generates a new one-time setup code and displays it on its local
console. The page accepts only a private IPv4 address and requires explicit
confirmation that the computer is dedicated and independent physical
safeguards exist. It invokes the existing signed appliance installer with an
argument vector, never a shell command. Installation remains simulator-safe and
does not commission equipment. On success, the one-time service disables itself
and redirects to the TLS-protected Admin GUI.

The signed release-directory installer remains supported for recovery and
development. AquaOS Core remains native under systemd; the image does not move
Docker or any UI into the critical control path.

## Consequences

Most users only write an image, boot it, choose the target disk, and finish in a
browser. Image production requires an isolated Debian build worker, `live-build`,
release signing, checksum publication, and physical BIOS/UEFI installation
tests. An image must not be advertised as production-ready until those tests,
upgrade/rollback, power-loss, and replacement-machine restoration pass.
