package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPAnthropicClient_RetriesOn429ThenSucceeds(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
		  "id":"msg_1",
		  "stop_reason":"end_turn",
		  "content":[{"type":"text","text":"{\"market_summary\":\"x\"}"}],
		  "usage":{"input_tokens":10,"output_tokens":20}
		}`))
	}))
	defer srv.Close()

	client := NewHTTPAnthropicClient("test-key", srv.URL)
	resp, err := client.CreateMessage(context.Background(), AnthropicMessageRequest{
		Model: "claude-test",
		System: "system",
		Messages: []AnthropicMessage{
			{Role: "user", Content: []AnthropicContentBlock{{Type: "text", Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("stop_reason: got %s want end_turn", resp.StopReason)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected retry path, got calls=%d", calls)
	}
}

func TestHTTPAnthropicClient_DoesNotRetryWhenContextDeadlineExceeded(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	client := NewHTTPAnthropicClient("test-key", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	_, err := client.CreateMessage(ctx, AnthropicMessageRequest{
		Model:  "claude-test",
		System: "system",
		Messages: []AnthropicMessage{
			{Role: "user", Content: []AnthropicContentBlock{{Type: "text", Text: "hello"}}},
		},
	})
	if err == nil {
		t.Fatal("expected context deadline error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected no retries on context deadline, calls=%d", calls)
	}
}
