package main

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"strings"
	"testing"

	"k8s.io/klog/v2"
)

func TestConfigureLoggingRejectsUnknownFormat(t *testing.T) {
	if err := configureLogging("yaml", &bytes.Buffer{}); err == nil {
		t.Fatal("expected an unknown log format to be rejected")
	}
	for _, format := range []string{"", "text", " TEXT "} {
		if err := configureLogging(format, &bytes.Buffer{}); err != nil {
			t.Fatalf("format %q: %v", format, err)
		}
	}
}

func TestConfigureLoggingJSONCoversLogAndKlog(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		klog.ClearLogger()
	})

	var output bytes.Buffer
	if err := configureLogging("json", &output); err != nil {
		t.Fatalf("configure json logging: %v", err)
	}
	log.Printf("NorthScope ready: snapshot v%d", 1)
	klog.ErrorS(nil, "Failed to watch", "resource", "services")
	klog.Flush()

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two JSON log lines, got %q", output.String())
	}
	for i, want := range []string{"NorthScope ready: snapshot v1", "Failed to watch"} {
		var entry map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &entry); err != nil {
			t.Fatalf("line %d is not JSON: %q", i, lines[i])
		}
		if entry["msg"] != want {
			t.Fatalf("line %d: expected msg %q, got %v", i, want, entry["msg"])
		}
	}
}
