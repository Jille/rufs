#!/bin/bash

set -e -o pipefail

cd $(dirname "$0")

# Build test binaries
mkdir -p bin
pushd bin
go generate ../../version
go build -tags withversion -o . ../../...
popd

go test
