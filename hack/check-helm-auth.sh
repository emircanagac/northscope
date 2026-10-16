#!/usr/bin/env bash
# Renders the Helm chart with each authentication mode and checks the output.
# Usage: hack/check-helm-auth.sh   (set HELM to use a helm binary not on PATH)

set -euo pipefail

helm="${HELM:-helm}"
root="$(cd "$(dirname "$0")/.." && pwd)"
chart="${root}/charts/northscope"
work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

render() {
  local output="$1"
  shift
  # Strip CR so the checks also work on checkouts with Windows line endings.
  "${helm}" template northscope "${chart}" --namespace northscope "$@" | tr -d '\r' >"${output}"
}

# render_notes <output> [helm args] renders NOTES.txt. helm template skips it
# and helm install --dry-run needs a cluster, so render it through a copy of
# the chart that exposes the notes as a template.
render_notes() {
  local output="$1"
  shift
  local notes_chart="${work_dir}/notes-chart"
  if [[ ! -d "${notes_chart}" ]]; then
    cp -r "${chart}" "${notes_chart}"
    mv "${notes_chart}/templates/NOTES.txt" "${notes_chart}/notes-source.txt"
    printf '%s\n' 'notes: {{ tpl (.Files.Get "notes-source.txt") . | b64enc }}' >"${notes_chart}/templates/notes.yaml"
  fi
  "${helm}" template northscope "${notes_chart}" --namespace northscope --show-only templates/notes.yaml "$@" |
    awk '$1 == "notes:" { print $2 }' | base64 -d | tr -d '\r' >"${output}"
}

# document <file> <kind> prints the rendered manifest of that kind.
document() {
  awk -v kind="$2" '
    /^---/ { if (match_doc) exit; doc = ""; next }
    { doc = doc $0 "\n" }
    $0 == "kind: " kind { match_doc = 1 }
    END { if (match_doc) printf "%s", doc }
  ' "$1"
}

# secret_value <file> <key> prints a decoded value of the rendered auth Secret.
secret_value() {
  document "$1" Secret | awk -v key="$2" '$1 == key ":" { print $2 }' | base64 -d
}

expect_contains() {
  grep --quiet --fixed-strings -- "$2" "$1" || fail "$3"
}

expect_missing() {
  if grep --quiet --fixed-strings -- "$2" "$1"; then
    fail "$3"
  fi
}

"${helm}" lint "${chart}" >/dev/null
"${helm}" lint "${chart}" --kube-version 1.20.15 >/dev/null

# Default: authentication on with a generated Secret.
default="${work_dir}/default.yaml"
render "${default}"
document "${default}" Secret >"${work_dir}/secret.yaml"
[[ -s "${work_dir}/secret.yaml" ]] || fail "default render has no auth Secret"
expect_contains "${work_dir}/secret.yaml" "name: northscope-auth" "auth Secret is not named <fullname>-auth"
[[ "$(secret_value "${default}" username)" == "admin" ]] || fail "default username is not admin"
password="$(secret_value "${default}" password)"
[[ "${password}" =~ ^[A-Za-z0-9]{24}$ ]] || fail "generated password is not 24 alphanumeric characters"
session_key="$(secret_value "${default}" session-key)"
[[ "${session_key}" =~ ^[A-Za-z0-9]{64}$ ]] || fail "generated session key is not 64 alphanumeric characters"
document "${default}" Deployment >"${work_dir}/deployment.yaml"
for expected in \
  '"-auth-dir=/etc/northscope/auth"' \
  '"-auth-cookie-secure=auto"' \
  '"-auth-protect-metrics=false"' \
  'mountPath: /etc/northscope/auth' \
  'readOnly: true' \
  'secretName: northscope-auth' \
  'key: session-key' \
  'readOnlyRootFilesystem: true' \
  'runAsNonRoot: true' \
  'allowPrivilegeEscalation: false'; do
  expect_contains "${work_dir}/deployment.yaml" "${expected}" "default Deployment is missing ${expected}"
done
# The checksum must match the rendered Secret data so a changed password
# rolls the Pod, and the generated values must be the same in both places.
secret_data="$(awk '/^data:$/ { in_data = 1; next } in_data && /^  / { sub(/^  /, ""); print }' "${work_dir}/secret.yaml")"
expected_checksum="$(printf '%s' "${secret_data}" | sha256sum | awk '{ print $1 }')"
expect_contains "${work_dir}/deployment.yaml" "checksum/auth: ${expected_checksum}" "Deployment auth checksum does not match the rendered Secret"
render_notes "${work_dir}/notes.txt"
expect_contains "${work_dir}/notes.txt" "Username: admin" "NOTES do not show the username"
expect_contains "${work_dir}/notes.txt" "kubectl -n northscope get secret northscope-auth -o jsonpath='{.data.password}' | base64 -d; echo" "NOTES do not show how to read the password"

# Explicit password.
explicit="${work_dir}/explicit.yaml"
render "${explicit}" --set auth.username=ops --set auth.password=chosen-password --set auth.cookieSecure=true --set auth.protectMetrics=true
[[ "$(secret_value "${explicit}" username)" == "ops" ]] || fail "auth.username is not used"
[[ "$(secret_value "${explicit}" password)" == "chosen-password" ]] || fail "auth.password is not used"
expect_contains "${explicit}" '"-auth-cookie-secure=true"' "auth.cookieSecure is not passed"
expect_contains "${explicit}" '"-auth-protect-metrics=true"' "auth.protectMetrics is not passed"

# Existing Secret: no chart-managed Secret or checksum.
existing="${work_dir}/existing.yaml"
render "${existing}" --set auth.existingSecret=platform-northscope-auth
[[ -z "$(document "${existing}" Secret)" ]] || fail "existingSecret render still creates a Secret"
expect_contains "${existing}" "secretName: platform-northscope-auth" "existingSecret is not mounted"
expect_missing "${existing}" "checksum/auth" "existingSecret render has a checksum of data it does not own"
render_notes "${work_dir}/existing-notes.txt" --set auth.existingSecret=platform-northscope-auth
expect_contains "${work_dir}/existing-notes.txt" "get secret platform-northscope-auth -o jsonpath='{.data.username}'" "existingSecret NOTES do not show how to read the username"

# Disabled: no Secret, volume, or auth arguments.
disabled="${work_dir}/disabled.yaml"
render "${disabled}" --set auth.enabled=false
[[ -z "$(document "${disabled}" Secret)" ]] || fail "auth.enabled=false still renders a Secret"
for unexpected in "-auth-" "volumes:" "volumeMounts:" "checksum/auth" "annotations:"; do
  expect_missing "${work_dir}/disabled.yaml" "${unexpected}" "auth.enabled=false render contains ${unexpected}"
done
render_notes "${work_dir}/disabled-notes.txt" --set auth.enabled=false
expect_contains "${work_dir}/disabled-notes.txt" "WARNING: Authentication is disabled" "disabled NOTES do not warn"

# Kubernetes 1.20 with production options.
legacy="${work_dir}/legacy.yaml"
render "${legacy}" --kube-version 1.20.15 --set ingress.enabled=true --set networkPolicy.enabled=true --set replicaCount=2 --set podDisruptionBudget.enabled=true
[[ -n "$(document "${legacy}" Secret)" ]] || fail "Kubernetes 1.20 render has no auth Secret"
expect_contains "${legacy}" '"-auth-dir=/etc/northscope/auth"' "Kubernetes 1.20 render does not enable auth"

# Pod annotations and the checksum share one annotations block.
annotated="${work_dir}/annotated.yaml"
render "${annotated}" --set podAnnotations.team=platform
document "${annotated}" Deployment | grep --quiet '^        team: platform$' || fail "podAnnotations are lost when auth adds a checksum"
document "${annotated}" Deployment | grep --quiet '^        checksum/auth: ' || fail "checksum is lost when podAnnotations are set"

# Invalid values fail the render.
if render "${work_dir}/invalid.yaml" --set auth.cookieSecure=sometimes 2>/dev/null; then
  fail "invalid auth.cookieSecure was accepted"
fi
if render "${work_dir}/invalid.yaml" --set auth.username= 2>/dev/null; then
  fail "empty auth.username was accepted"
fi

echo "Helm authentication render checks passed."
