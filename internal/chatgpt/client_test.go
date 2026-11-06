package chatgpt

import (
	"bytes"
	"context"
	"crypto/sha3"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stream read past DONE") }

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureClient(fn roundTripFunc) *Client {
	return &Client{http: &http.Client{Transport: fn}, token: "private-token", sessionID: "fixture-session",
		fp: fingerprint{UserAgent: "Firefox fixture", DeviceID: "fixture-device", Language: "zh-CN", Languages: "zh-CN,en", Script: "https://chatgpt.com/backend-api/sentinel/sdk.js", Screen: 2000, Cores: 8, Timezone: "Asia/Shanghai", Offset: -480}}
}

func reply(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestProofMatchesChallengeAndHonorsCancellation(t *testing.T) {
	config := (fingerprint{UserAgent: "Firefox"}).proofConfig()
	token, err := proofToken(context.Background(), "fixture-seed", "00", config)
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.TrimPrefix(token, "gAAAAAB")
	digest := sha3.Sum512([]byte("fixture-seed" + encoded))
	if !strings.HasPrefix(token, "gAAAAAB") || digest[0] != 0 {
		t.Fatalf("invalid proof: %x", digest)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	var values []any
	if err != nil || json.Unmarshal(decoded, &values) != nil || len(values) != 25 || values[24] != float64(1) {
		t.Fatal("proof lost Firefox identity")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := proofToken(ctx, "seed", "00", config); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, difficulty := range []string{"", "xyz", "000", strings.Repeat("00", 65)} {
		if _, err := proofToken(context.Background(), "seed", difficulty, config); err == nil {
			t.Fatal(difficulty)
		}
	}
}

func TestNativeImageProtocolAndReferenceIsolation(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(fmt.Sprint(edit), func(t *testing.T) {
			var source bytes.Buffer
			if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(t.TempDir(), "参考图.png")
			if err := os.WriteFile(input, source.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			var paths []string
			if edit {
				paths = []string{input, input}
			}
			var calls []string
			uploads := 0
			client := fixtureClient(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.URL.Hostname() == "chatgpt.com" {
					if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("Oai-Device-Id") != "fixture-device" || r.Header.Get("X-OpenAI-Target-Path") != r.URL.Path {
						t.Fatal("account headers missing")
					}
				} else if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("credentials leaked to storage")
				}
				var payload map[string]any
				if r.Method == http.MethodPost {
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
				}
				switch r.URL.Path {
				case "/backend-api/files":
					uploads++
					if payload["file_name"] != "参考图.png" || payload["width"] != float64(32) {
						t.Fatal(payload)
					}
					return reply(200, fmt.Sprintf(`{"file_id":"file-input-%d","upload_url":"https://fixture.blob.core.windows.net/input-%d?sig=private"}`, uploads, uploads))
				case "/input-1", "/input-2":
					body, _ := io.ReadAll(r.Body)
					if r.Method != http.MethodPut || !bytes.Equal(body, source.Bytes()) || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("X-Ms-Blob-Type") != "BlockBlob" {
						t.Fatal("upload changed bytes or headers")
					}
					return reply(201, "")
				case "/backend-api/files/file-input-1/uploaded", "/backend-api/files/file-input-2/uploaded":
					return reply(200, `{}`)
				case "/backend-api/sentinel/chat-requirements/prepare":
					if !strings.HasPrefix(payload["p"].(string), "gAAAAAC") {
						t.Fatal(payload)
					}
					return reply(200, `{"prepare_token":"prepare","proofofwork":{"required":true,"seed":"seed","difficulty":"ff"}}`)
				case "/backend-api/sentinel/chat-requirements/finalize":
					proof := strings.TrimPrefix(payload["proof_token"].(string), "gAAAAAB")
					if _, err := base64.StdEncoding.DecodeString(proof); err != nil || payload["prepare_token"] != "prepare" {
						t.Fatal("invalid finalize")
					}
					return reply(200, `{"token":"requirements"}`)
				case "/backend-api/f/conversation/prepare":
					return reply(200, `{"conduit_token":"conduit"}`)
				case "/backend-api/f/conversation":
					if payload["model"] != "gpt-5-5" || r.Header.Get("X-Conduit-Token") != "conduit" || r.Header.Get("Openai-Sentinel-Chat-Requirements-Token") != "requirements" {
						t.Fatal("generation protocol missing")
					}
					messages := payload["messages"].([]any)
					content := messages[0].(map[string]any)["content"].(map[string]any)
					if len(content["parts"].([]any)) != 1+len(paths) {
						t.Fatal("reference count changed")
					}
					return reply(200, "data: {\"v\":{\"conversation_id\":\"conversation-1\",\"message\":{\"author\":{\"role\":\"user\"},\"content\":{\"parts\":[\"file-service://file-input-1\"]}}}}\n\ndata: {\"p\":\"/message/content/parts/0\",\"v\":\"file-service://file-result\"}\n\ndata: [DONE]\n\n")
				case "/backend-api/conversation/conversation-1":
					return reply(200, `{"mapping":{"user":{"message":{"author":{"role":"user"},"content":{"parts":["file-service://file-input-1"]}}},"result":{"message":{"author":{"role":"tool"},"content":{"parts":[{"asset_pointer":"file-service://file-result"}]}}}}}`)
				case "/backend-api/files/file-result/download":
					return reply(200, `{"download_url":"https://files.oaiusercontent.com/generated.png?sig=private"}`)
				case "/generated.png":
					return reply(200, string(source.Bytes()))
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
					return nil, nil
				}
			})
			g := &Generation{}
			if err := client.Start(context.Background(), g, "画一只猫", DefaultModel, paths); err != nil {
				t.Fatal(err)
			}
			if !g.Submitted || !g.Attempted || g.ConversationID != "conversation-1" || !slices.Equal(g.files, []string{"file-result"}) {
				t.Fatalf("state %+v", g)
			}
			urls, err := client.Results(context.Background(), g)
			if err != nil || len(urls) != 1 {
				t.Fatalf("results %v %v", urls, err)
			}
			resp, err := client.request(context.Background(), http.MethodGet, urls[0], nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || !bytes.Equal(data, source.Bytes()) {
				t.Fatal("download changed bytes", err)
			}
			before := len(calls)
			if err := client.Start(context.Background(), g, "same", DefaultModel, nil); !errors.Is(err, ErrSubmissionUncertain) || len(calls) != before {
				t.Fatal("submitted task was resent")
			}
		})
	}
}

func TestSubmissionUncertaintyPreservesOriginalConversation(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantID     string
		wantErr    error
	}{
		{"lost-id", "data: [DONE]\n\n", "", ErrSubmissionUncertain},
		{"partial-stream", "data: {\"conversation_id\":\"kept-id\"}\n\n", "kept-id", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fixtureClient(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/backend-api/sentinel/chat-requirements/prepare":
					return reply(200, `{"prepare_token":"p"}`)
				case "/backend-api/sentinel/chat-requirements/finalize":
					return reply(200, `{"token":"t"}`)
				case "/backend-api/f/conversation/prepare":
					return reply(200, `{"conduit_token":"c"}`)
				default:
					return reply(200, test.body)
				}
			})
			g := &Generation{}
			err := client.Start(context.Background(), g, "cat", DefaultModel, nil)
			if !errors.Is(err, test.wantErr) || g.ConversationID != test.wantID || !g.Attempted {
				t.Fatalf("state=%+v err=%v", g, err)
			}
		})
	}
}

func TestResultsWaitsForEveryKnownImage(t *testing.T) {
	for _, attachment := range []bool{false, true} {
		for _, neverReady := range []bool{false, true} {
			t.Run(fmt.Sprintf("attachment=%t/never-ready=%t", attachment, neverReady), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					pendingCalls := 0
					g := &Generation{ConversationID: "original", Attempted: true, Submitted: true, files: []string{"ready", "pending"}}
					if attachment {
						g.files, g.sediments = nil, g.files
					}
					client := fixtureClient(func(r *http.Request) (*http.Response, error) {
						if r.Method != http.MethodGet {
							t.Fatalf("result recovery attempted a new submission: %s", r.Method)
						}
						switch {
						case strings.HasSuffix(r.URL.Path, "/ready/download"):
							return reply(200, `{"download_url":"https://files.oaiusercontent.com/ready.png"}`)
						case strings.HasSuffix(r.URL.Path, "/pending/download"):
							pendingCalls++
							if neverReady || pendingCalls == 1 {
								return reply(404, `{}`)
							}
							return reply(200, `{"download_url":"https://files.oaiusercontent.com/pending.png"}`)
						case r.URL.Path == "/backend-api/conversation/original", r.URL.Path == "/backend-api/tasks":
							return reply(200, `{}`)
						default:
							t.Fatalf("unexpected request %s", r.URL.Path)
							return nil, nil
						}
					})
					ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
					defer cancel()
					urls, err := client.Results(ctx, g)
					if neverReady {
						if !errors.Is(err, context.DeadlineExceeded) {
							t.Fatalf("incomplete results reported success: urls=%v err=%v", urls, err)
						}
					} else if err != nil || !slices.Equal(urls, []string{"https://files.oaiusercontent.com/ready.png", "https://files.oaiusercontent.com/pending.png"}) {
						t.Fatalf("known output lost: urls=%v err=%v", urls, err)
					}
					if pendingCalls != 2 || g.ConversationID != "original" || !g.Submitted {
						t.Fatalf("recovery lost the original generation: calls=%d state=%+v", pendingCalls, g)
					}
				})
			})
		}
	}
}

func TestStreamQuotaRejectionAllowsAnotherAccount(t *testing.T) {
	starts := 0
	client := fixtureClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/backend-api/sentinel/chat-requirements/prepare":
			return reply(200, `{"prepare_token":"p"}`)
		case "/backend-api/sentinel/chat-requirements/finalize":
			return reply(200, `{"token":"t"}`)
		case "/backend-api/f/conversation/prepare":
			return reply(200, `{"conduit_token":"c"}`)
		default:
			starts++
			if starts == 1 {
				return reply(200, "data: {\"error\":{\"code\":\"image_generation_limit\"}}\n\n")
			}
			return reply(200, "data: {\"conversation_id\":\"accepted\"}\n\n")
		}
	})
	g := &Generation{}
	if err := client.Start(context.Background(), g, "cat", DefaultModel, nil); !errors.Is(err, ErrQuota) || g.Attempted || g.Submitted {
		t.Fatalf("quota state=%+v err=%v", g, err)
	}
	if err := client.Start(context.Background(), g, "cat", DefaultModel, nil); err != nil || !g.Submitted || starts != 2 {
		t.Fatalf("retry state=%+v starts=%d err=%v", g, starts, err)
	}
}

func TestStreamStopsAtDoneBeforeConnectionCloses(t *testing.T) {
	g := &Generation{}
	stream := io.MultiReader(strings.NewReader("data: {\"conversation_id\":\"kept\"}\n\ndata: [DONE]\n\n"), failingReader{})
	if err := g.readStream(stream); err != nil || g.ConversationID != "kept" {
		t.Fatalf("id=%s error=%v", g.ConversationID, err)
	}
}

func TestTrustBoundariesAndAccountFailures(t *testing.T) {
	for _, raw := range []string{"https://chatgpt.com/", "https://files.oaiusercontent.com/a?sig=a%2Fb", "https://a.blob.core.windows.net/a"} {
		if !allowedURL(raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"http://chatgpt.com/", "https://chatgpt.com.evil/a", "https://evil.com/a", "https://127.0.0.1/a", "https://user:pass@chatgpt.com/", "https://chatgpt.com:8080/", "file:///tmp/a"} {
		if allowedURL(raw) {
			t.Fatal(raw)
		}
	}
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{401, `{}`, ErrNeedLogin}, {403, `{}`, ErrVerification}, {429, `{"code":"image_generation_limit"}`, ErrQuota},
		{429, `{}`, ErrRate}, {400, `{"code":"content_policy_violation"}`, ErrModeration},
		{429, `{"code":"image_generation_rate_limit_exceeded"}`, ErrRate},
	} {
		if !errors.Is(responseError(tc.status, []byte(tc.body)), tc.want) {
			t.Fatal(tc)
		}
	}
	client := fixtureClient(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("secret URL %s and private-token", r.URL)
	})
	if _, err := client.request(context.Background(), http.MethodGet, "https://files.oaiusercontent.com/a?sig=private", nil, nil); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("error leaked a credential")
	}
	pendingClient := fixtureClient(func(*http.Request) (*http.Response, error) { return reply(200, `{}`) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := pendingClient.Results(ctx, &Generation{ConversationID: "pending"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("poll did not stop")
	}
}
