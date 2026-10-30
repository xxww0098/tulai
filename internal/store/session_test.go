package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.db")
	kept := filepath.Join(dir, "kept.png")
	if err := os.WriteFile(kept, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().Unix()
	id, err := s.SaveActive(Session{
		Page: PageGen, Prompts: "窗边的猫\n傍晚的街", Images: "/ref/a.png\n/ref/b.png",
		Progress: 0.5, ProgText: "1 / 2",
	}, []SessionTask{
		{Ord: 1, Prompt: "窗边的猫", Source: "/ref/a.png", Platform: "豆包", AccountID: 7, AccountName: "豆包主力", Status: "成功", Outputs: kept + "\n/out/b.png", At: at},
		{Ord: 2, Prompt: "傍晚的街", Platform: "千问", Status: "失败", Error: "上传失败", At: at},
	})
	if err != nil || id == 0 {
		t.Fatalf("save id=%d err=%v", id, err)
	}
	again, err := s.SaveActive(Session{
		ID: id, Page: PageGen, Prompts: "窗边的猫\n傍晚的街", Images: "/ref/a.png",
		Progress: 1, ProgText: "2 / 2",
	}, []SessionTask{
		{Ord: 1, Prompt: "窗边的猫", Platform: "豆包", AccountID: 7, AccountName: "豆包主力", Status: "成功", Outputs: kept, At: at},
	})
	if err != nil || again != id {
		t.Fatalf("update id=%d (%d) err=%v", again, id, err)
	}
	var active int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE page=? AND active=1`, PageGen).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active gen sessions=%d err=%v", active, err)
	}

	washID, err := s.SaveActive(Session{Page: PageWash, Prompts: "白底", Images: "/wash/a.png"}, []SessionTask{
		{Ord: 1, Prompt: "白底", Source: "/wash/a.png", Platform: "豆包", Status: "等待", At: at},
	})
	if err != nil || washID == 0 {
		t.Fatal(err)
	}

	sess, items, ok, err := s.ActiveSession(PageGen)
	if err != nil || !ok {
		t.Fatalf("active gen ok=%v err=%v", ok, err)
	}
	if sess.ID != id || sess.Prompts != "窗边的猫\n傍晚的街" || sess.Images != "/ref/a.png" || sess.ProgText != "2 / 2" || sess.Progress != 1 {
		t.Fatalf("session %+v", sess)
	}
	if len(items) != 1 || items[0].Outputs != kept || items[0].AccountName != "豆包主力" || items[0].AccountID != 7 || items[0].At != at {
		t.Fatalf("tasks %+v", items)
	}
	if _, _, ok, err := s.ActiveSession(PageWash); err != nil || !ok {
		t.Fatalf("wash active ok=%v err=%v", ok, err)
	}
	if hist, err := s.History(PageGen); err != nil || len(hist) != 0 {
		t.Fatalf("active session leaked into history: %+v %v", hist, err)
	}

	if err := s.ArchiveSession(id); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := s.ActiveSession(PageGen); err != nil || ok {
		t.Fatalf("archived session still active ok=%v err=%v", ok, err)
	}
	hist, err := s.History(PageGen)
	if err != nil || len(hist) != 1 || hist[0].ID != id || hist[0].Tasks != 1 || hist[0].Prompts == "" {
		t.Fatalf("history %+v %v", hist, err)
	}
	if err := s.ActivateSession(id); err != nil {
		t.Fatal(err)
	}
	if sess, _, ok, err := s.ActiveSession(PageGen); err != nil || !ok || sess.ID != id {
		t.Fatalf("reactivated %+v ok=%v err=%v", sess, ok, err)
	}
	if hist, err := s.History(PageGen); err != nil || len(hist) != 0 {
		t.Fatalf("history after activate %+v %v", hist, err)
	}

	if err := s.ArchiveSession(id); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("delete removed the image: %v", err)
	}
	if _, _, err := s.Session(id); err == nil {
		t.Fatal("deleted session still loads")
	}
	if err := s.DeleteSession(0); err == nil {
		t.Fatal("delete of 0 should fail")
	}
	if _, _, ok, err := s.ActiveSession(PageWash); err != nil || !ok || washID == 0 {
		t.Fatalf("wash session was removed ok=%v err=%v", ok, err)
	}

	s.Close()
	reopen, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopen.Close()
	loaded, loadedTasks, ok, err := reopen.ActiveSession(PageWash)
	if err != nil || !ok || loaded.ID != washID || loaded.Prompts != "白底" || len(loadedTasks) != 1 || loadedTasks[0].Source != "/wash/a.png" {
		t.Fatalf("reopen %+v tasks=%+v ok=%v err=%v", loaded, loadedTasks, ok, err)
	}
}
