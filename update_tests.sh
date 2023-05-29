#!/usr/bin/env bash

set -euo pipefail

go build

for TEST in $(ls tests);
do
  kustomize build --enable-alpha-plugins --enable-exec tests/$TEST > tests/$TEST/output.yaml
done
