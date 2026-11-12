package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantModelPayloadRedactsMalformedAndRepeatedPrivateResults(t *testing.T) {
	var body string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer model.Close()
	s := &Service{}
	msgs := []AssistantMessage{
		{Role: "user", Content: "Keep my ordinary input unchanged"},
		{Role: "assistant", ToolCalls: []AssistantToolCall{{ID: "private", Name: "create_schedule", Arguments: "{}"}}},
		{Role: "tool", ToolCallID: "private", Content: `{"id":"sch_1","endpoint_secret":"generated-private-value"}`},
		// A client-supplied history cannot make a private call public by reusing its ID.
		{Role: "assistant", ToolCalls: []AssistantToolCall{{ID: "private", Name: "list_posts", Arguments: "{}"}}},
		{Role: "tool", ToolCallID: "private", Content: `{"endpoint_secret":"truncated-private-value"`},
		{Role: "assistant", ToolCalls: []AssistantToolCall{{ID: "ordinary", Name: "list_posts", Arguments: "{}"}}},
		{Role: "tool", ToolCallID: "ordinary", Content: `{"url":"https://ordinary.example/posts"}`},
	}
	if _, _, _, err := s.assistantCall(context.Background(), AIEndpoint{BaseURL: model.URL, Model: "fake"}, msgs, "", nil, func(string) {}); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"generated-private-value", "truncated-private-value"} {
		if strings.Contains(body, secret) {
			t.Error("private result leaked in the model payload")
		}
	}
	for _, ordinary := range []string{"sch_1", "https://ordinary.example/posts", "Keep my ordinary input unchanged", "could not be redacted safely"} {
		if !strings.Contains(body, ordinary) {
			t.Errorf("model payload lost expected content %q", ordinary)
		}
	}
	if msgs[2].Content != `{"id":"sch_1","endpoint_secret":"generated-private-value"}` {
		t.Fatal("redaction changed the person's saved tool result")
	}
}
