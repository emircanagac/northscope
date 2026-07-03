package server

import (
	"net/http"
	"testing"
)

func TestSameOriginAllowsMissingOrigin(t *testing.T) {
	request := &http.Request{
		Host:   "northscope.local",
		Header: http.Header{},
	}

	if !sameOrigin(request) {
		t.Fatal("expected missing Origin header to be allowed")
	}
}

func TestSameOriginAllowsMatchingHost(t *testing.T) {
	request := &http.Request{
		Host: "northscope.local:8080",
		Header: http.Header{
			"Origin": []string{"http://northscope.local:8080"},
		},
	}

	if !sameOrigin(request) {
		t.Fatal("expected matching Origin host to be allowed")
	}
}

func TestSameOriginRejectsCrossOrigin(t *testing.T) {
	request := &http.Request{
		Host: "northscope.local:8080",
		Header: http.Header{
			"Origin": []string{"https://example.com"},
		},
	}

	if sameOrigin(request) {
		t.Fatal("expected cross-origin websocket request to be rejected")
	}
}
