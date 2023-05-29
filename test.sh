#!/usr/bin/env bash

set -euo pipefail

go build

for TEST in $(ls tests);
do
  echo "Test: $TEST"
  diff --color tests/$TEST/output.yaml <(kustomize build --enable-alpha-plugins --enable-exec tests/$TEST)
  echo "OK"
done
