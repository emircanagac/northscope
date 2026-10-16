#!/usr/bin/env bash

set -euo pipefail

image="${1:-northscope:test}"
expected_version="${2:-dev}"
container_name="northscope-runtime-smoke"
auth_container_name="northscope-runtime-smoke-auth"
work_dir="$(mktemp -d)"
api_pid=""

cleanup() {
  docker rm -f "${container_name}" "${auth_container_name}" >/dev/null 2>&1 || true
  if [[ -n "${api_pid}" ]]; then
    kill "${api_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${work_dir}"
}
trap cleanup EXIT

python3 hack/fake-kube-api.py 19090 &
api_pid=$!

cat >"${work_dir}/kubeconfig" <<'EOF'
apiVersion: v1
kind: Config
clusters:
  - name: smoke
    cluster:
      server: http://127.0.0.1:19090
contexts:
  - name: smoke
    context:
      cluster: smoke
      user: smoke
current-context: smoke
users:
  - name: smoke
    user: {}
EOF

docker run --detach \
  --name "${container_name}" \
  --network host \
  --volume "${work_dir}/kubeconfig:/tmp/kubeconfig:ro" \
  "${image}" \
  -addr=:18080 \
  -kubeconfig=/tmp/kubeconfig \
  -gateway-api=false \
  -f5=false \
  >/dev/null

for _ in $(seq 1 30); do
  if curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null; then
    break
  fi
  sleep 1
done

# expect_match <pattern> <command...> captures the whole output before matching.
# Piping into grep --quiet would let grep exit at the first match and kill the
# writer with SIGPIPE, which pipefail reports as a failure.
expect_match() {
  local pattern="$1"
  shift
  local output
  output="$("$@" 2>&1)"
  if ! grep --quiet -- "${pattern}" <<<"${output}"; then
    echo "expected output of '$*' to match ${pattern}" >&2
    exit 1
  fi
}

curl --fail --silent http://127.0.0.1:18080/healthz >/dev/null
curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null
expect_match "\"version\":\"${expected_version}\"" curl --fail --silent http://127.0.0.1:18080/api/version
expect_match '<div id="root"></div>' curl --fail --silent http://127.0.0.1:18080/
expect_match 'using EndpointSlice API discovery.k8s.io/v1$' docker logs "${container_name}"
expect_match 'WARNING: authentication is disabled' docker logs "${container_name}"

runtime_user="$(docker inspect --format '{{.Config.User}}' "${container_name}")"
if [[ "${runtime_user}" != "65532:65532" ]]; then
  echo "unexpected runtime user: ${runtime_user}" >&2
  exit 1
fi

# Authentication enabled through a mounted credentials directory, as the Helm
# chart mounts its Secret.
auth_dir="${work_dir}/auth"
mkdir -p "${auth_dir}"
printf 'admin\n' >"${auth_dir}/username"
printf 'smoke-password\n' >"${auth_dir}/password"
head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' >"${auth_dir}/session-key"
chmod 755 "${auth_dir}"
chmod 644 "${auth_dir}"/*

docker run --detach \
  --name "${auth_container_name}" \
  --network host \
  --volume "${work_dir}/kubeconfig:/tmp/kubeconfig:ro" \
  --volume "${auth_dir}:/etc/northscope/auth:ro" \
  "${image}" \
  -addr=:18081 \
  -kubeconfig=/tmp/kubeconfig \
  -gateway-api=false \
  -f5=false \
  -auth-dir=/etc/northscope/auth \
  >/dev/null

auth_base="http://127.0.0.1:18081"
for _ in $(seq 1 30); do
  if curl --fail --silent "${auth_base}/readyz" >/dev/null; then
    break
  fi
  sleep 1
done

status() {
  curl --silent --output /dev/null --write-out '%{http_code}' "$@"
}

expect_status() {
  local want="$1"
  shift
  local got
  got="$(status "$@")"
  if [[ "${got}" != "${want}" ]]; then
    echo "expected HTTP ${want} from curl $*, got ${got}" >&2
    exit 1
  fi
}

cookie_jar="${work_dir}/cookies"
curl --fail --silent "${auth_base}/healthz" >/dev/null
curl --fail --silent "${auth_base}/readyz" >/dev/null
expect_status 401 "${auth_base}/api/version"
expect_status 302 "${auth_base}/"
expect_match '<form method="post" action="/login">' curl --fail --silent "${auth_base}/login"
expect_status 401 --data 'username=admin&password=wrong' "${auth_base}/login"
expect_status 303 --cookie-jar "${cookie_jar}" --data 'username=admin&password=smoke-password' "${auth_base}/login"
expect_match "\"version\":\"${expected_version}\"" curl --fail --silent --cookie "${cookie_jar}" "${auth_base}/api/version"
expect_match '"authEnabled":true' curl --fail --silent --cookie "${cookie_jar}" "${auth_base}/api/session"
expect_match 'authentication enabled for user "admin"' docker logs "${auth_container_name}"

echo "NorthScope runtime image smoke test passed."
