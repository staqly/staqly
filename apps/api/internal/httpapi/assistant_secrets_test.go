package httpapi_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func TestAssistantKeepsGeneratedSecretsOutOfModelRequests(t *testing.T) {
	for _, name := range []string{"create_schedule", "update_schedule", "create_channel_link", "create_preview_link"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.paid()
			e.key = e.admin
			var col app.Collection
			e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "Assistant secrets"}, 201, &col)
			args := map[string]any{}
			switch name {
			case "create_schedule", "update_schedule":
				args = map[string]any{"collection": col.ID, "name": "Endpoint", "kind": "recurring", "rule": map[string]any{"every_minutes": 60},
					"source": "endpoint", "endpoint_url": "https://endpoint.example/posts", "enabled": false}
				if name == "update_schedule" {
					var schedule app.Schedule
					e.do(nil, "POST", "/v1/schedules", e.key, args, 201, &schedule)
					args = map[string]any{"schedule_id": schedule.ID, "endpoint_secret": ""}
				}
			case "create_preview_link":
				var post app.Post
				e.do(nil, "POST", "/v1/posts", e.key, map[string]any{"collection": col.ID, "title": "Private draft", "content": "Private draft"}, 201, &post)
				args["post_id"] = post.ID
			}
			sent := make(chan []byte, 8)
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				sent <- body
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Created.\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer model.Close()
			e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: model.URL + "/v1", Model: "fake", Prices: &[2]float64{0, 0}}}
			rawArgs, _ := json.Marshal(args)
			messages := []app.AssistantMessage{{Role: "user", Content: "Set this up"}, {Role: "assistant", ToolCalls: []app.AssistantToolCall{{ID: "secret_call", Name: name, Arguments: string(rawArgs)}}}}
			const chatID = "ach_secrets0001"
			events := e.assistantRequest(app.AssistantRequest{ChatID: chatID, Messages: messages, Decision: &app.AssistantDecision{ToolCallID: "secret_call", Allow: true}})
			var result map[string]string
			for _, event := range events {
				if event.Type == "error" || (event.Type == "tool" && !event.OK) {
					t.Fatalf("tool did not remain available: %+v", event)
				}
				if event.Type == "message" && event.Message != nil {
					messages = append(messages, *event.Message)
					if event.Message.Role == "tool" {
						var values map[string]json.RawMessage
						if err := json.Unmarshal([]byte(event.Message.Content), &values); err != nil {
							t.Fatal(err)
						}
						result = map[string]string{}
						for _, key := range []string{"id", "endpoint_secret", "token", "url"} {
							var value string
							_ = json.Unmarshal(values[key], &value)
							result[key] = value
						}
					}
				}
			}
			secret := result["endpoint_secret"]
			if strings.Contains(name, "link") {
				secret = result["token"]
				if !strings.Contains(result["url"], secret) {
					t.Fatal("the person's link no longer carries its token")
				}
			}
			if len(secret) < 16 || strings.HasPrefix(secret, "****") || result["id"] == "" {
				t.Fatal("the person lost the generated secret or result id")
			}
			check := func(turn string, wantID bool) {
				t.Helper()
				select {
				case body := <-sent:
					if strings.Contains(string(body), secret) {
						t.Errorf("%s: generated secret/token was sent to the model", turn)
					}
					if wantID && !strings.Contains(string(body), result["id"]) {
						t.Errorf("%s: ordinary tool output was lost", turn)
					}
				default:
					t.Fatal("fake model received no request")
				}
			}
			check("after tool", true)
			var chat app.AssistantChat
			e.do(nil, "GET", "/v1/assistant/chats/"+chatID, e.key, nil, 200, &chat)
			if len(chat.Messages) != 4 || !strings.Contains(chat.Messages[2].Content, secret) {
				t.Fatal("the person lost the private result in their saved chat")
			}
			messages = append(chat.Messages, app.AssistantMessage{Role: "user", Content: "Summarize what changed"})
			e.assistantRequest(app.AssistantRequest{ChatID: chatID, Messages: messages})
			check("continued history", true)

			// The 200-message saved history can retain the tool result without its older call.
			messages = messages[:3]
			for range 198 {
				messages = append(messages, app.AssistantMessage{Role: "user", Content: "Continue"})
			}
			e.assistantRequest(app.AssistantRequest{ChatID: chatID, Messages: messages})
			check("before history trimming", false)
			e.do(nil, "GET", "/v1/assistant/chats/"+chatID, e.key, nil, 200, &chat)
			if len(chat.Messages) != 200 || chat.Messages[0].Role != "tool" || !strings.Contains(chat.Messages[0].Content, secret) {
				t.Fatal("test did not retain a private result at the saved history boundary")
			}
			messages = append(chat.Messages, app.AssistantMessage{Role: "user", Content: "Summarize again"})
			e.assistantRequest(app.AssistantRequest{Messages: messages})
			check("replayed trimmed history", false)
		})
	}
}
