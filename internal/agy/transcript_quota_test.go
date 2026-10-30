package agy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTranscript(t *testing.T, home, id, body string) {
	t.Helper()
	dir := filepath.Join(cliDir(home), "brain", id, ".system_generated", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptQuotaFindsSubagent429(t *testing.T) {
	home := t.TempDir()
	started := time.Now().Add(-time.Minute)
	writeTranscript(t, home, "main", `{"status":"DONE","content":"已通知"}`+"\n")
	writeTranscript(t, home, "sub", `{"status":"ERROR","error":"image failed to generate: no image generated in response"}`+"\n"+
		`{"status":"ERROR","error":"image failed to generate: failed to generate content: 429 Too Many Requests, body: {\"error\":{\"status\":\"RESOURCE_EXHAUSTED\"}}"}`+"\n")
	if ok, _ := TranscriptQuota(home, started, time.Now()); !ok {
		t.Fatal("子对话里的 429 应当算额度用尽")
	}
}

func TestTranscriptQuotaIgnoresOldAndOtherErrors(t *testing.T) {
	home := t.TempDir()
	writeTranscript(t, home, "sub", `{"status":"ERROR","error":"no image generated in response"}`+"\n")
	if ok, _ := TranscriptQuota(home, time.Now().Add(-time.Minute), time.Now()); ok {
		t.Fatal("普通失败不算额度")
	}
	writeTranscript(t, home, "old", `{"status":"ERROR","error":"429 Too Many Requests"}`+"\n")
	if ok, _ := TranscriptQuota(home, time.Now().Add(time.Hour), time.Now()); ok {
		t.Fatal("运行开始前的记录不算")
	}
}
