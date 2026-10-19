package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

func TestSendStableKeyAndChannelIsolation(t *testing.T) {
	for _, channel := range []string{"chat", "email"} {
		t.Run(channel, func(t *testing.T) {
			n := &repository.ParentNotification{ID: uuid.New(), TenantID: uuid.New(), ParentID: uuid.New(), Channel: channel, SenderEmail: "sender@example.test", RecipientEmail: "parent@example.test", Body: "Ani: kelas berubah 20-10-2026 09:00 WIB"}
			var previous, key string
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				data, _ := io.ReadAll(r.Body)
				var body map[string]any
				if err := json.Unmarshal(data, &body); err != nil {
					t.Error(err)
				}
				current := r.Header.Get("Idempotency-Key")
				if channel == "chat" {
					current, _ = body["idempotency_key"].(string)
					if r.URL.Path != "/internal/notifications" || r.Header.Get("X-Internal-Service-Credential") != "internal-secret" || r.Header.Get("Authorization") != "" || body["parent_user_id"] != n.ParentID.String() || body["tenant_id"] != n.TenantID.String() {
						t.Error("wrong chat recipient/auth")
					}
					if _, ok := body["to"]; ok {
						t.Error("email address leaked to chat")
					}
				} else {
					if r.Header.Get("Authorization") != "Bearer resend-secret" || r.Header.Get("X-Internal-Service-Credential") != "" || body["text"] != n.Body {
						t.Error("wrong email auth/content")
					}
				}
				if current != "academic-notification/"+n.ID.String() {
					t.Error("missing stable key")
				}
				if calls > 1 && (string(data) != previous || current != key) {
					t.Error("retry changed payload or key")
				}
				previous, key = string(data), current
				if calls == 1 {
					w.WriteHeader(503)
					fmt.Fprint(w, "DO NOT EXPOSE PROVIDER SECRET")
					return
				}
				if channel == "chat" {
					fmt.Fprint(w, `{"data":{"id":"message"}}`)
				} else {
					fmt.Fprint(w, `{"id":"email"}`)
				}
			}))
			defer server.Close()
			c := NewClient(server.URL, "internal-secret", "resend-secret")
			c.resendURL = server.URL + "/emails"
			if err := c.Send(context.Background(), n); err != ErrUnavailable {
				t.Fatalf("unexpected error %v", err)
			}
			if err := c.Send(context.Background(), n); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSendRejectsMalformedRedirectAndCancellation(t *testing.T) {
	n := &repository.ParentNotification{ID: uuid.New(), Channel: "email", SenderEmail: "sender@example.test", RecipientEmail: "parent@example.test", Body: "text"}
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"malformed", "not JSON", 200}, {"missing id", `{}`, 200}, {"bad status", `{"id":"email"}`, 400}, {"redirect", "", 302}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c := NewClient("", "", "key")
			c.resendURL = server.URL
			if c.Send(context.Background(), n) != ErrUnavailable {
				t.Fatal("uncertain delivery was accepted")
			}
			if calls != 1 {
				t.Fatal("redirect followed")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if c.Send(ctx, n) != ErrUnavailable {
				t.Fatal("cancel ignored")
			}
		})
	}
	if NewClient("", "", "").Send(context.Background(), n) != ErrUnavailable {
		t.Fatal("missing key accepted")
	}
}
