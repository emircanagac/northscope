#!/usr/bin/env bash

set -euo pipefail

image="${1:-northscope:test}"
container_name="northscope-runtime-smoke"
work_dir="$(mktemp -d)"
api_pid=""

cleanup() {
  docker rm -f "${container_name}" >/dev/null 2>&1 || true
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

curl --fail --silent http://127.0.0.1:18080/healthz >/dev/null
curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null
curl --fail --silent http://127.0.0.1:18080/ | grep --quiet '<div id="root"></div>'

runtime_user="$(docker inspect --format '{{.Config.User}}' "${container_name}")"
if [[ "${runtime_user}" != "65532:65532" ]]; then
  echo "unexpected runtime user: ${runtime_user}" >&2
  exit 1
fi

echo "NorthScope runtime image smoke test passed."
