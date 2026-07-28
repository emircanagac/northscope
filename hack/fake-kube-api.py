#!/usr/bin/env python3

import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit


RESOURCE_KINDS = {
    "/api/v1/endpoints": ("v1", "EndpointsList"),
    "/api/v1/nodes": ("v1", "NodeList"),
    "/api/v1/pods": ("v1", "PodList"),
    "/api/v1/services": ("v1", "ServiceList"),
    "/apis/discovery.k8s.io/v1/endpointslices": (
        "discovery.k8s.io/v1",
        "EndpointSliceList",
    ),
    "/apis/networking.k8s.io/v1/ingressclasses": (
        "networking.k8s.io/v1",
        "IngressClassList",
    ),
    "/apis/networking.k8s.io/v1/ingresses": (
        "networking.k8s.io/v1",
        "IngressList",
    ),
}


class KubernetesHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = urlsplit(self.path).path
        resource = next(
            (
                details
                for suffix, details in RESOURCE_KINDS.items()
                if path.endswith(suffix)
            ),
            None,
        )
        if resource is None:
            self.send_error(404)
            return

        api_version, kind = resource
        payload = json.dumps(
            {
                "apiVersion": api_version,
                "kind": kind,
                "metadata": {"resourceVersion": "1"},
                "items": [],
            }
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, _format, *_args):
        return


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 19090
    ThreadingHTTPServer(("127.0.0.1", port), KubernetesHandler).serve_forever()


if __name__ == "__main__":
    main()
