#!/bin/bash
umount /var/tmp/yoink 2>/dev/null || true
cryptsetup close yoink-crypt 2>/dev/null || true
rm -f /var/tmp/yoink.luks
rm -f /tmp/yoink.key
