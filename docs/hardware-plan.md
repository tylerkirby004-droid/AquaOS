# Hardware plan

The supported controller is one dedicated x86-64 computer running Debian
stable. A small business-class mini PC with wired Ethernet, 4 CPU threads,
8 GB RAM, and a 128 GB SSD is sufficient for the standard profile. Use at least
16 GB RAM and a 512 GB endurance SSD for `--advanced-history`.

The controller, Ethernet/PoE switch, router, and critical LAN devices should be
covered by a correctly sized UPS. Reserve the controller's address in DHCP and
keep encrypted backups on a different physical machine or removable medium.

Record the controller model, storage health, Debian version, network address,
UPS runtime, device firmware, wiring, backup destination, and replacement
procedure. A spare computer capable of restoring the latest backup materially
reduces recovery time.

Independent heater thermostats, overflow/ATO limits, appropriate GFCI/RCD and
grounding, and safe equipment defaults are mandatory. The dedicated appliance
is still one failure domain and must not be the only barrier against harm.
