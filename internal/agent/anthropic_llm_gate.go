package agent

import (
	"context"
	"fmt"
	"sync"
)

// SerializedAnthropicClient serializes Anthropic calls in-process to reduce concurrent TPM spikes.
type SerializedAnthropicClient struct {
	Inner AnthropicClient
	mu    sync.Mutex
}

func (s *SerializedAnthropicClient) CreateMessage(ctx context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	if s == nil || s.Inner == nil {
		return AnthropicMessageResponse{}, fmt.Errorf("serialized anthropic client: not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Inner.CreateMessage(ctx, req)
}
