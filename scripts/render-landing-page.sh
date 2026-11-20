#!/usr/bin/env bash
# render-landing-page.sh <Chart.yaml> <chart version> <output file>
#
# Renders the Helm repository landing page (.github/pages/index.html) for the
# given released chart. The release workflow runs it for every tag, and the
# pages workflow runs it when the template changes on main, always with the
# Chart.yaml of the latest release.
set -euo pipefail

chart="$1"
version="$2"
output="$3"
template="$(dirname "$0")/../.github/pages/index.html"

app_version="$(awk '/^appVersion:/ {print $2; exit}' "$chart" | tr -d '"')"
kube_version="$(awk '/^kubeVersion:/ {print $2; exit}' "$chart" | tr -d '"' | sed -E 's/^>=([0-9]+\.[0-9]+).*/\1/')"
if [ -z "$app_version" ] || [ -z "$kube_version" ]; then
  echo "::error::Could not read appVersion or kubeVersion from $chart" >&2
  exit 1
fi

sed -e "s/@CHART_VERSION@/${version}/g" -e "s/@APP_VERSION@/${app_version}/g" -e "s/@KUBE_VERSION@/${kube_version}/g" \
  "$template" > "$output"
if grep --quiet '@[A-Z_]*@' "$output"; then
  echo "::error::Unresolved placeholder in the landing page" >&2
  exit 1
fi
