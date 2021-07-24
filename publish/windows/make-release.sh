#!/bin/bash

set -e

cd "$(dirname "$0")"

RUFS_VERSION="$1"
if [ -z "$RUFS_VERSION" ]; then
	RUFS_VERSION=$(git describe --tags --dirty --always | awk -F- '{print $1}' | cut -b 2- -)
fi

# Clean up
rm -f rufs.exe rufs-setup.exe

# Build application
go generate ../../version
GOOS=windows GOARCH=amd64 go build -tags withversion -ldflags -H=windowsgui -o rufs.exe ../../client

# Build installer
makensis -DRUFS_VERSION="${RUFS_VERSION}" rufs-setup.nsi
