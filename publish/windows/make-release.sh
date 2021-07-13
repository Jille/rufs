#!/bin/bash

set -e

cd "$(dirname "$0")"

# Clean up
rm -f rufs.exe rufs-setup.exe

# Build application
go generate ../../version
GOOS=windows GOARCH=amd64 go build -tags withversion -ldflags -H=windowsgui -o rufs.exe ../../client

# Build installer
makensis rufs-setup.nsi
