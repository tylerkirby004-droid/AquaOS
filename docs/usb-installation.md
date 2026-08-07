# Install AquaOS from a USB drive

This is the easiest supported installation path. You need a dedicated x86-64
computer, an 8 GB or larger USB drive, wired Ethernet, and another computer
with a web browser. Installing erases the disk you select.

> AquaOS remains pre-release. Use the simulator and disconnected test loads
> until the physical safety, firmware, power-loss, restore, and soak tests pass.

## Make the USB drive

For the current pre-release test image, open the repository's **Actions** tab,
select **Build appliance ISO**, open the latest successful run, and download the
`aquaos-test-iso` artifact. GitHub packages the ISO and verification files in a
ZIP archive. This image uses a per-build development signing key and is only for
installation and simulator testing.

Once a hardware-validated production release exists, download
`aquaos-VERSION-amd64.iso` and its `.sha256` file from the AquaOS release instead.

1. Extract the downloaded artifact.
2. Verify the checksum with your operating system's checksum tool.
3. Write the ISO to the USB drive with Raspberry Pi Imager, balenaEtcher, or
   another image writer. Select the AquaOS ISO as a custom image.
4. Safely eject the USB drive.

Never use an image from an unofficial download or continue after a checksum
mismatch.

## Install on the dedicated computer

1. Disconnect aquarium equipment from controlled outlets.
2. Connect the dedicated computer to wired Ethernet, a monitor, and keyboard.
3. Insert the USB drive and boot from it. The boot-menu key is commonly F12,
   F11, Esc, or Del.
4. Choose **Install AquaOS**.
5. Choose language and keyboard and create the requested local recovery account.
6. Select the dedicated computer's internal disk. Read the erase confirmation
   carefully. Do not select a disk containing anything you need.
7. When installation finishes, remove the USB drive and reboot.

The installer is based on the standard Debian Installer and intentionally keeps
disk selection and the destructive confirmation visible.

## Finish in a browser

After the new computer starts, its screen shows a one-time setup code. On
another device connected to the same trusted LAN, open:

```text
https://aquaos.local:8443
```

The temporary certificate is generated on that computer, so the browser may
ask you to approve a local certificate warning. Enter the displayed setup code,
enter the private address currently assigned and reserved in your router,
choose the timezone, and accept
the two safety statements.

Leave **Advanced long-term history** off for the simplest installation. Home
Assistant still provides normal sensor and equipment history. Enable it only
when you want InfluxDB retention and Grafana analysis and have at least 16 GB
RAM and a 512 GB SSD.

Select **Install AquaOS** and keep the page open. The signed installer creates
new credentials unique to this computer, installs native Core under systemd,
and starts Home Assistant and Mosquitto. It never commissions equipment. When
complete, the page opens AquaOS Admin.

Then create the Home Assistant owner, add its MQTT integration using the
generated credentials, configure an off-host backup, and use AquaOS Admin for
discovery, calibration, alarm setup, bench testing, and commissioning.

If `aquaos.local` does not resolve, use the private address displayed by your
router with port 8443. For recovery and diagnostic installation, follow the
[signed command-line procedure](installation.md).

## Release-builder instructions

End users do not build the image. A release maintainer uses a disposable Debian
stable amd64 builder with Go and `live-build` installed:

```sh
sudo apt install live-build debootstrap
sudo env AQUAOS_VERSION=vVERSION \
  ./scripts/build-appliance-image.sh dist/signed-release dist/image
```

The script verifies the signed-release Core digest, builds the temporary
first-boot binary, produces a hybrid BIOS/UEFI ISO, and writes an ISO checksum.
The project must additionally sign and publish the ISO/checksum using the
release process. Test the exact artifact on clean physical BIOS and UEFI
machines before publishing it as production-ready.
