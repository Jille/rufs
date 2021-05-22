#!/bin/bash

set -e

# Clean up
rm -f rufs.exe rufs-setup.exe

# Build application
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o rufs.exe ../../client

# Build installer
makensis rufs-setup.nsi
