#!/bin/sh

cd "${0%/*}"

cleanup() {
	err=$?
	if [ -d "$HOME/rufs" ]; then
		umount "$HOME/rufs"
		rmdir "$HOME/rufs"
	fi
	exit $err
}

trap cleanup EXIT INT QUIT TERM

mkdir -p "$HOME/rufs"
./client \
	--mountpoint "$HOME/rufs"
