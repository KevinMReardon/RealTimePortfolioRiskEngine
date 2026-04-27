package agent

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

var (
	secretKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|token|authorization|bearer|private[_-]?key|access[_-]?key|client[_-]?secret)`)
	bearerPattern    = regexp.MustCompile(`(?i)bearer\s+[a-z0-9\-\._~\+\/]+=*`)
	kvPattern        = regexp.MustCompile(`(?i)(api[_-]?key|token|password|secret)\s*[:=]\s*([^\s,;]+)`)
)

func redactJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`)
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return json.RawMessage(RedactText(string(raw)))
	}
	redacted := redactValue("", obj)
	out, err := json.Marshal(redacted)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

func RedactText(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	s = bearerPattern.ReplaceAllString(s, "bearer [REDACTED]")
	s = kvPattern.ReplaceAllString(s, "$1=[REDACTED]")
	return s
}

func redactValue(key string, v any) any {
	switch vv := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(vv))
		for k, child := range vv {
			if secretKeyPattern.MatchString(k) {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = redactValue(k, child)
		}
		return out
	case []any:
		out := make([]any, 0, len(vv))
		for _, child := range vv {
			out = append(out, redactValue(key, child))
		}
		return out
	case string:
		if secretKeyPattern.MatchString(key) {
			return "[REDACTED]"
		}
		if secretKeyPattern.MatchString(vv) && len(vv) > 24 {
			return "[REDACTED]"
		}
		return RedactText(vv)
	default:
		return vv
	}
}
