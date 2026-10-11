#!/bin/sh
# Installed at: /config/scripts/post-config.d/internet-block.sh
# Runs on every boot (post EdgeOS config). /etc does not survive firmware
# upgrades, but /config does — so we re-install the systemd unit each boot.
set -eu

SRC_UNIT=/config/internet-block/internet-block.service
DST_UNIT=/etc/systemd/system/internet-block.service

if [ ! -f "$SRC_UNIT" ]; then
    echo "internet-block: missing unit at $SRC_UNIT" >&2
    exit 1
fi

install -m 0644 "$SRC_UNIT" "$DST_UNIT"
systemctl daemon-reload
systemctl enable --now internet-block.service
