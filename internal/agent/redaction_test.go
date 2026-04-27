package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactJSON_RedactsSecretKeysAndBearerValues(t *testing.T) {
	t.Parallel()
	in := json.RawMessage(`{
		"api_key":"abc123",
		"nested":{"password":"hunter2","note":"bearer sk-live-supersecret"},
		"safe":"hello"
	}`)
	out := string(redactJSON(in))
	if strings.Contains(out, "abc123") || strings.Contains(out, "hunter2") || strings.Contains(out, "sk-live-supersecret") {
		t.Fatalf("redacted output leaked secret: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected redaction marker, got: %s", out)
	}
}

func TestRedactText_RedactsKeyValueTokens(t *testing.T) {
	t.Parallel()
	in := "token=shh123 api_key:abcd bearer aaa.bbb.ccc"
	out := RedactText(in)
	if strings.Contains(out, "shh123") || strings.Contains(out, "abcd") || strings.Contains(out, "aaa.bbb.ccc") {
		t.Fatalf("RedactText leaked secret: %q", out)
	}
}
