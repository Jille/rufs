#!/bin/bash

vagrant upload vagrant-test.sh vagrant-test.sh rufs-client >/dev/null
read_overhead_sum="0"
runs=5
for i in `seq 1 ${runs}`; do
    rm -f ../public/output.json
    vagrant ssh rufs-client -c 'bash ./vagrant-test.sh' >test-output.log 2>&1
    if [ "$(cat ../public/output.json | jq '.success')" != "true" ]; then
        echo "Test run failed:"
        cat test-output.log
        exit 1
    fi
    rm test-output.log
    read_overhead="$(cat ../public/output.json | jq '.metrics.read_overhead')"
    read_overhead_sum="${read_overhead_sum} + ${read_overhead}"
done

echo "Test SUCCEEDED - average rufs read overhead: $(echo -e "scale=4\n($read_overhead_sum) / ${runs}" | bc -l)"
