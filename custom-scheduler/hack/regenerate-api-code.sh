#!/bin/bash

# Exit on error
set -e

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../../code-generator)}

# Generate deepcopy, conversion, defaulter functions
bash "${CODEGEN_PKG}"/generate-groups.sh "deepcopy,defaulter,conversion" \
  sigs.k8s.io/scheduler-plugins/apis/config/v1 \
  sigs.k8s.io/scheduler-plugins/apis \
  "config:v1" \
  --go-header-file "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
  --output-base "${SCRIPT_ROOT}"

echo "API code regeneration complete" 