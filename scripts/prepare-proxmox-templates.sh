#!/bin/sh
set -eu

usage() {
  echo "usage: $0 --debian-image FILE --debian-sha256 HEX --haos-image FILE --haos-sha256 HEX --storage NAME --bridge NAME --debian-vmid ID --haos-vmid ID [--apply]" >&2
}

apply=false
debian_image= debian_sha= haos_image= haos_sha= storage= bridge= debian_vmid= haos_vmid=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --debian-image) debian_image=$2; shift 2 ;;
    --debian-sha256) debian_sha=$2; shift 2 ;;
    --haos-image) haos_image=$2; shift 2 ;;
    --haos-sha256) haos_sha=$2; shift 2 ;;
    --storage) storage=$2; shift 2 ;;
    --bridge) bridge=$2; shift 2 ;;
    --debian-vmid) debian_vmid=$2; shift 2 ;;
    --haos-vmid) haos_vmid=$2; shift 2 ;;
    --apply) apply=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then echo "run on the Proxmox node as root" >&2; exit 1; fi
for value in "$debian_image" "$debian_sha" "$haos_image" "$haos_sha" "$storage" "$bridge" "$debian_vmid" "$haos_vmid"; do
  if [ -z "$value" ]; then usage; exit 2; fi
done
case "$storage$bridge$debian_vmid$haos_vmid" in *[!A-Za-z0-9._-]*) echo "identifier contains unsupported characters" >&2; exit 1;; esac
printf '%s  %s\n' "$debian_sha" "$debian_image" | sha256sum --check --status
printf '%s  %s\n' "$haos_sha" "$haos_image" | sha256sum --check --status
if qm status "$debian_vmid" >/dev/null 2>&1 || qm status "$haos_vmid" >/dev/null 2>&1; then
  echo "a requested template VM ID already exists; nothing was changed" >&2
  exit 1
fi

echo "Will create Debian template $debian_vmid and HAOS template $haos_vmid on storage $storage using bridge $bridge."
if [ "$apply" != true ]; then
  echo "Dry run only. Review the values, then repeat with --apply."
  exit 0
fi

qm create "$debian_vmid" --name aquaos-debian-template --memory 2048 --cores 2 --net0 "virtio,bridge=$bridge" --scsihw virtio-scsi-pci
qm importdisk "$debian_vmid" "$debian_image" "$storage"
qm set "$debian_vmid" --scsi0 "$storage:vm-$debian_vmid-disk-0" --ide2 "$storage:cloudinit" --boot order=scsi0 --serial0 socket --vga serial0 --agent enabled=1
qm template "$debian_vmid"

qm create "$haos_vmid" --name aquaos-haos-template --memory 4096 --cores 2 --net0 "virtio,bridge=$bridge" --scsihw virtio-scsi-pci --machine q35 --bios ovmf
qm importdisk "$haos_vmid" "$haos_image" "$storage"
qm set "$haos_vmid" --scsi0 "$storage:vm-$haos_vmid-disk-0" --efidisk0 "$storage:0,efitype=4m,pre-enrolled-keys=1" --boot order=scsi0 --agent enabled=1
qm template "$haos_vmid"
echo "Templates created. Keep the source URLs, checksums, and versions in your installation record."
