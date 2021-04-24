#!/bin/bash

set -e

dd if=/dev/urandom of=/data/file bs=50M count=1

bin/client -mountpoint /fuse &
trap "fusermount -u /fuse" EXIT

DEADLINE=$(($(date +%s) + 10))
while :; do
	if [ -d /fuse/data ]; then
		break
	fi
	if [ $(date +%s) -ge $DEADLINE ]; then
		echo "File system did not appear in time"
		exit 1
	fi
	sleep 0.1
done

# Read file from disk, so it enters cache.
sha256sum /data/file >/dev/null

ORIG_START=$(date +%s.%6N)
ORIG_SUM=$(sha256sum /data/file | awk '{print $1}')
ORIG_STOP=$(date +%s.%6N)
RUFS_START=$(date +%s.%6N)
RUFS_SUM=$(sha256sum /fuse/data/file | awk '{print $1}')
RUFS_STOP=$(date +%s.%6N)

jq -n \
  --arg orig_start "$ORIG_START" \
  --arg orig_sum "$ORIG_SUM" \
  --arg orig_stop "$ORIG_STOP" \
  --arg rufs_start "$RUFS_START" \
  --arg rufs_sum "$RUFS_SUM" \
  --arg rufs_stop "$RUFS_STOP" \
  '{"success": ($orig_sum == $rufs_sum), "metrics": { "read_overhead": ((($rufs_stop|tonumber) - ($rufs_start|tonumber)) / (($orig_stop|tonumber) - ($orig_start|tonumber))) }}' \
  > /public/output.json

