package aicore_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valpere/ragivka/pkg/aicore"
)

// TestOllamaClient_successfulResponse verifies that a 200 response with a valid
// Ollama JSON body is correctly parsed into a GenerateResponse.
func TestOllamaClient_successfulResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body := map[string]any{
			"model":             "qwen3.5:cloud",
			"message":           map[string]any{"role": "assistant", "content": "Hello!"},
			"done":              true,
			"prompt_eval_count": 100,
			"eval_count":        50,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer ts.Close()

	client := aicore.NewOllamaClient(aicore.OllamaConfig{
		APIURL: ts.URL,
		Model:  "test-model",
	})

	got, err := client.Generate(context.Background(), aicore.GenerateRequest{
		Messages: []aicore.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "Hello!" {
		t.Errorf("content: got %q, want %q", got.Content, "Hello!")
	}
	if got.Model != "qwen3.5:cloud" {
		t.Errorf("model: got %q, want %q", got.Model, "qwen3.5:cloud")
	}
	if got.InputTokens != 100 {
		t.Errorf("input tokens: got %d, want 100", got.InputTokens)
	}
	if got.OutputTokens != 50 {
		t.Errorf("output tokens: got %d, want 50", got.OutputTokens)
	}
}

// TestOllamaClient_non200Response verifies that a non-200 HTTP status code is
// surfaced as an error rather than silently succeeding.
func TestOllamaClient_non200Response(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer ts.Close()

	client := aicore.NewOllamaClient(aicore.OllamaConfig{
		APIURL: ts.URL,
		Model:  "test-model",
	})

	_, err := client.Generate(context.Background(), aicore.GenerateRequest{
		Messages: []aicore.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

// TestOllamaClient_contextCancellation verifies that cancelling the request
// context mid-flight causes Generate to return an error promptly.
func TestOllamaClient_contextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// handlerStarted is closed by the handler when it starts processing, so
	// the test knows the request is in-flight before it cancels the context.
	handlerStarted := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(handlerStarted)
		// Block until the test-level context is cancelled or a safety timeout fires.
		// We use ctx (closed over from the test) rather than r.Context() because
		// Go's HTTP/1.1 server does not reliably cancel r.Context() the moment the
		// client drops the connection; it only detects closure during the next TCP
		// poll, which can take several seconds.  Using the test context guarantees
		// the handler unblocks immediately when cancel() is called, preventing
		// httptest.Server.Close() from blocking for its 5-second internal timeout.
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := aicore.NewOllamaClient(aicore.OllamaConfig{
		APIURL: ts.URL,
		Model:  "test-model",
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := client.Generate(ctx, aicore.GenerateRequest{
			Messages: []aicore.Message{{Role: "user", Content: "hi"}},
		})
		errCh <- err
	}()

	// Wait until the handler is actually processing the request, then cancel.
	<-handlerStarted
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after context cancellation, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Generate to return after context cancellation")
	}
}

// TestOllamaClient_malformedJSONResponse verifies that a 200 response with a
// body that is not valid JSON surfaces a parse error.
func TestOllamaClient_malformedJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid json {{{"))
	}))
	defer ts.Close()

	client := aicore.NewOllamaClient(aicore.OllamaConfig{
		APIURL: ts.URL,
		Model:  "test-model",
	})

	_, err := client.Generate(context.Background(), aicore.GenerateRequest{
		Messages: []aicore.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for malformed JSON response, got nil")
	}
}
