#!/bin/bash

set -e -o pipefail

cd $(dirname "$0")

# Build test binaries
mkdir -p bin
pushd bin
go generate ../../version
GOOS=linux GOARCH=amd64 go build -tags withversion -o . ../../...
popd

# Start over with new state
mkdir -p public
rm -rf public/*

# Rebuild the VMs
vagrant up --provision

echo "======================="
echo "==  RuFS Test suite  =="
echo "======================="

# Run tests
for testdir in [1234567890]*; do
  pushd $testdir >/dev/null
  echo -n "$testdir: "
  ./run-test.sh
  popd >/dev/null
done

# Clean up
echo -e "\nNote: Vagrant VMs are still running. Clean up with \`vagrant destroy\`."
