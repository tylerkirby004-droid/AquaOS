# Scripts

`build-appliance-image.sh` turns a signed amd64 release directory into the
hybrid USB installer ISO used by new operators. Run it only on an isolated
Debian release builder with `live-build`; see `docs/usb-installation.md`.

Operational and release scripts belong here. Bootstrap, migration, and recovery
workflows include `release-acceptance.sh`; it runs only the automated portion of
Prompt 14 and never marks physical or elapsed-time evidence complete.
scripts must be idempotent and documented before they are added.
