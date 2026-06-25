package agent

import (
	"strings"
)

// IsBriefingRateLimited reports provider TPM / rate-limit errors from briefing runs.
func IsBriefingRateLimited(err error) bool {
	return isAnthropicRateLimit(err)
}

func isAnthropicRateLimit(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 429") || strings.Contains(msg, "rate_limit")
}
