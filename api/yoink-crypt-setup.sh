#!/bin/bash
set -e

BACKING=/var/tmp/yoink.luks
MOUNT=/var/tmp/yoink
KEYFILE=/tmp/yoink.key
MAPPER=yoink-crypt
SIZE=200G  # sparse, only uses actual space written

# generate random key (stored in /tmp = tmpfs = RAM only)
dd if=/dev/urandom of="$KEYFILE" bs=32 count=1 2>/dev/null
chmod 600 "$KEYFILE"

# remove old container if exists
umount "$MOUNT" 2>/dev/null || true
cryptsetup close "$MAPPER" 2>/dev/null || true
rm -f "$BACKING"

# create sparse backing file
truncate -s "$SIZE" "$BACKING"

# format + open LUKS
cryptsetup luksFormat --batch-mode "$BACKING" "$KEYFILE"
cryptsetup open "$BACKING" "$MAPPER" --key-file="$KEYFILE"

# create filesystem + mount
mkfs.ext4 -q /dev/mapper/"$MAPPER"
mkdir -p "$MOUNT"
mount /dev/mapper/"$MAPPER" "$MOUNT"
chmod 1777 "$MOUNT"

echo "yoink encrypted storage ready"
