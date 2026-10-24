package extbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVocabularyMatchesSpec(t *testing.T) {
	if strings.Join(Types(), ",") != "cmd,ack,event,ping,pong,hello,error" {
		t.Fatalf("types: %s", strings.Join(Types(), ","))
	}
	if strings.Join(Commands(), ",") != "ping,helloAck,upload,setPrompt,send,waitResult,download,captchaAssist,cancel,status" {
		t.Fatalf("commands: %s", strings.Join(Commands(), ","))
	}
	wantCodes := "E_NOT_CONNECTED,E_NO_TAB,E_NOT_LOGGED_IN,E_SELECTOR_MISS,E_UPLOAD_FAIL,E_SEND_UNCERTAIN,E_TIMEOUT,E_CAPTCHA_HUMAN,E_CAPTCHA_SOLVE_FAIL,E_DOWNLOAD_FAIL,E_SW_SLEEP,E_PROTOCOL,E_AUTH,E_CANCELLED"
	if strings.Join(ErrorCodes(), ",") != wantCodes {
		t.Fatalf("codes: %s", strings.Join(ErrorCodes(), ","))
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"name":"upload","paths":["a.png"]}`)
	in := Envelope{
		V: Version, ID: "id-1", Type: TypeCmd, TS: 10,
		Platform: PlatformQianwen, SessionID: "s", TaskID: "t",
		IdempotencyKey: "k", Payload: payload,
	}
	raw, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{`"v":1`, `"id":"id-1"`, `"type":"cmd"`, `"sessionId":"s"`, `"taskId":"t"`, `"idempotencyKey":"k"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("missing %s in %s", field, text)
		}
	}
	out, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.V != in.V || out.ID != in.ID || out.Type != in.Type || out.TS != in.TS ||
		out.Platform != in.Platform || out.SessionID != in.SessionID || out.TaskID != in.TaskID ||
		out.IdempotencyKey != in.IdempotencyKey || !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("round trip: %+v", out)
	}
	ping, err := Encode(Envelope{V: Version, ID: "p", Type: TypePing, TS: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ping), "idempotency") || strings.Contains(string(ping), "payload") {
		t.Fatalf("empty optional fields were encoded: %s", ping)
	}
}

func TestDecodeRejectsBadEnvelopes(t *testing.T) {
	cases := []string{
		``,
		`{`,
		`{"v":2,"id":"a","type":"ping","ts":1}`,
		`{"v":1,"type":"ping","ts":1}`,
		`{"v":1,"id":"a","type":"nope","ts":1}`,
		`{"v":1,"id":"a","type":"cmd","ts":1,"payload":{"name":"nope"}}`,
		`{"v":1,"id":"a","type":"hello","ts":1,"platform":"qianwen","payload":{}}`,
		`{"v":1,"id":"a","type":"error","ts":1,"payload":{}}`,
		`{"v":1,"id":"a","type":"ping","ts":1,"platform":"other"}`,
	}
	for _, raw := range cases {
		_, err := Decode([]byte(raw))
		if CodeOf(err) != EProtocol {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
}

func TestCommandAndErrorHelpers(t *testing.T) {
	env, err := Command(CmdSetPrompt, map[string]any{"text": "一只猫"})
	if err != nil {
		t.Fatal(err)
	}
	env.ID = "c"
	env.TS = 1
	raw, err := Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	name, err := out.Name()
	if err != nil || name != CmdSetPrompt {
		t.Fatalf("name: %s %v", name, err)
	}
	err = &ProtoError{Code: ETimeout, Message: "slow"}
	if !errors.Is(err, &ProtoError{Code: ETimeout}) || CodeOf(err) != ETimeout {
		t.Fatalf("errors.Is: %v", err)
	}
	if NormalizeBrowserBackend(" Extension ") != "extension" || NormalizeBrowserBackend("Chrome") != "ego" || NormalizeBrowserBackend("") != "ego" {
		t.Fatal("browserBackend normalize")
	}
	if SettingBrowserBackend != "browserBackend" {
		t.Fatal(SettingBrowserBackend)
	}
}

func TestClassifyConnErr(t *testing.T) {
	if CodeOf(classifyConnErr(io.EOF)) != ENotConnected {
		t.Fatal(classifyConnErr(io.EOF))
	}
	if CodeOf(classifyConnErr(os.ErrDeadlineExceeded)) != ESWSleep {
		t.Fatal(classifyConnErr(os.ErrDeadlineExceeded))
	}
}

func TestExtensionSkeletonMatchesProtocol(t *testing.T) {
	root := repoRoot(t)
	protocol := mustRead(t, filepath.Join(root, "extension", "protocol.js"))
	for _, name := range append(append([]string{}, Commands()...), ErrorCodes()...) {
		if !bytes.Contains(protocol, []byte(name)) {
			t.Errorf("protocol.js missing %s", name)
		}
	}
	if !bytes.Contains(protocol, []byte("15000")) {
		t.Fatal("protocol.js ping interval")
	}
	background := mustRead(t, filepath.Join(root, "extension", "background.js"))
	for _, needle := range []string{"COMMAND_FROM_HOST", "chrome.alarms", "pingEveryMs", "periodInMinutes: 0.5"} {
		if !bytes.Contains(background, []byte(needle)) {
			t.Errorf("background.js missing %s", needle)
		}
	}
	content := mustRead(t, filepath.Join(root, "extension", "content.js"))
	for _, needle := range []string{"COMMAND_FROM_HOST", "E_SELECTOR_MISS", "E_SEND_UNCERTAIN", "DataTransfer", "idempotencyKey"} {
		if !bytes.Contains(content, []byte(needle)) && !bytes.Contains(mustRead(t, filepath.Join(root, "extension", "qianwen", "page.js")), []byte(needle)) {
			t.Errorf("qianwen extension missing %s", needle)
		}
	}
	page := mustRead(t, filepath.Join(root, "extension", "qianwen", "page.js"))
	for _, needle := range []string{"添加附件", "上传图片", "DataTransfer", "#qk-markdown-react", "captchaBlocked"} {
		if !bytes.Contains(page, []byte(needle)) && !bytes.Contains(content, []byte(needle)) {
			t.Errorf("qianwen page actions missing %s", needle)
		}
	}
	var manifest map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(root, "extension", "manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["manifest_version"] != float64(3) {
		t.Fatalf("manifest: %#v", manifest["manifest_version"])
	}
	perms, _ := manifest["permissions"].([]any)
	need := map[string]bool{"storage": true, "tabs": true, "alarms": true, "scripting": true}
	for _, item := range perms {
		delete(need, item.(string))
	}
	if len(need) != 0 {
		t.Fatalf("missing permissions: %v", need)
	}
	hosts, _ := json.Marshal(manifest["host_permissions"])
	for _, needle := range []string{"www.tongyi.com", "qianwen.com", "chat.qwen.ai", "*.qwen.ai", "www.doubao.com", "127.0.0.1"} {
		if !bytes.Contains(hosts, []byte(needle)) {
			t.Errorf("host_permissions missing %s: %s", needle, hosts)
		}
	}
	scripts, _ := json.Marshal(manifest["content_scripts"])
	for _, needle := range []string{"www.tongyi.com", "qianwen.com", "chat.qwen.ai", "qianwen/page.js", "doubao/page.js", "www.doubao.com", "content.js"} {
		if !bytes.Contains(scripts, []byte(needle)) {
			t.Errorf("content_scripts missing %s: %s", needle, scripts)
		}
	}
	if !bytes.Contains(background, []byte("tongyi.com")) || !bytes.Contains(background, []byte("qianwen.com")) || !bytes.Contains(background, []byte("doubao.com")) {
		t.Fatal("background tab patterns do not cover tongyi.com, qianwen.com, and doubao.com")
	}
	doubaoPage := mustRead(t, filepath.Join(root, "extension", "doubao", "page.js"))
	for _, needle := range []string{"upload_file_button", "上传文件或图片", "mdbox_image", "message_image_content", "captchaBlocked", "DataTransfer"} {
		if !bytes.Contains(doubaoPage, []byte(needle)) && !bytes.Contains(content, []byte(needle)) {
			t.Errorf("doubao page actions missing %s", needle)
		}
	}
}

func TestExtensionScriptsParse(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"extension/content.js",
		"extension/background.js",
		"extension/protocol.js",
		"extension/options.js",
		"extension/doubao/page.js",
		"extension/qianwen/page.js",
	} {
		out, err := exec.Command("node", "--check", filepath.Join(root, rel)).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", rel, err, out)
		}
	}
}

func TestQianwenPageSelectors(t *testing.T) {
	root := repoRoot(t)
	out, err := exec.Command("node", filepath.Join(root, "extension", "qianwen", "page_test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("page_test.js: %v\n%s", err, out)
	}
}

func TestDoubaoPageSelectors(t *testing.T) {
	root := repoRoot(t)
	out, err := exec.Command("node", filepath.Join(root, "extension", "doubao", "page_test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("doubao page_test.js: %v\n%s", err, out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
