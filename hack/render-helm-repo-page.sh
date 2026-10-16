#!/usr/bin/env bash
# Renders the Helm chart repository landing page published to GitHub Pages.

set -euo pipefail

usage="usage: hack/render-helm-repo-page.sh <chart-version> <output-file>"
chart_version="${1:?${usage}}"
output="${2:?${usage}}"
root="$(cd "$(dirname "$0")/.." && pwd)"
chart="${root}/charts/northscope/Chart.yaml"

app_version="$(awk '/^appVersion:/ {print $2; exit}' "${chart}" | tr -d '"')"
kube_version="$(
  awk '/^kubeVersion:/ {print $2; exit}' "${chart}" |
    tr -d '"' |
    sed -E 's/^>=([0-9]+\.[0-9]+).*/\1/'
)"

sed \
  -e "s/@CHART_VERSION@/${chart_version}/g" \
  -e "s/@APP_VERSION@/${app_version}/g" \
  -e "s/@KUBE_VERSION@/${kube_version}/g" \
  "${root}/.github/pages/index.html" >"${output}"

if grep --quiet '@[A-Z_]*@' "${output}"; then
  echo "unresolved placeholder in ${output}" >&2
  exit 1
fi
