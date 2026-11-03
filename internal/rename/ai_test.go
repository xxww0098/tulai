package rename

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSuggest(t *testing.T) {
	const key = "test-secret-key"
	for _, tc := range []struct {
		name, content string
		status        int
		want          []string
	}{
		{"plain", `["first.png","第二张.jpg"]`, 200, []string{"first.png", "第二张.jpg"}},
		{"fenced", "```json\n[\"first.png\",\"第二张.jpg\"]\n```", 200, []string{"first.png", "第二张.jpg"}},
		{"count", `["only.png"]`, 200, nil},
		{"empty", `["first.png"," "]`, 200, nil},
		{"invalid", `not json`, 200, nil},
		{"http error", key, 401, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+key {
					t.Errorf("unexpected request: %s %q", r.Method, r.Header.Get("Authorization"))
				}
				var request struct {
					Model    string `json:"model"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "model" || len(request.Messages) != 2 ||
					!strings.Contains(request.Messages[1].Content, "按时间命名") || !strings.Contains(request.Messages[1].Content, "old.png") {
					t.Errorf("unexpected payload: %+v, %v", request, err)
				}
				w.WriteHeader(tc.status)
				if tc.status != 200 {
					_, _ = fmt.Fprint(w, tc.content)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": tc.content}}}})
			}))
			defer server.Close()
			got, err := Suggest(context.Background(), server.URL, key, "model", "按时间命名", []string{"old.png", "old.jpg"})
			if tc.want == nil {
				if err == nil || strings.Contains(err.Error(), key) {
					t.Fatalf("expected a safe error, got %q, %v", got, err)
				}
			} else if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestSuggestRejectsCrossOriginRedirectAndOversize(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if _, err := Suggest(context.Background(), redirect.URL, "key", "model", "name", []string{"a"}); err == nil || hits.Load() != 0 {
		t.Fatalf("cross-origin redirect: error=%v, hits=%d", err, hits.Load())
	}

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
	}))
	defer big.Close()
	if _, err := Suggest(context.Background(), big.URL, "key", "model", "name", []string{"a"}); err == nil {
		t.Fatal("oversized response was accepted")
	}
}
