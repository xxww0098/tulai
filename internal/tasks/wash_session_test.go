package tasks

import (
	"testing"

	"github.com/xxww0098/tulai/internal/cdp"
)

func TestWashChatReuseStopsAtBoundaryAndOnFailure(t *testing.T) {
	e := &Engine{}
	first, other := &cdp.Session{}, &cdp.Session{}
	const url = "https://www.doubao.com/chat/one"
	for i := 1; i <= washChatTasks; i++ {
		e.recordWashChat(first, url)
		chat := e.washChats[first]
		if chat.count != i || chat.reusable(url, 1) != (i < washChatTasks) {
			t.Fatalf("count=%d chat=%+v", i, chat)
		}
	}
	e.recordWashChat(other, url)
	if !e.washChats[other].reusable(url, 1) || e.washChats[other].reusable(url, 2) || e.washChats[other].reusable("", 1) || e.washChats[other].reusable(url+"-other", 1) {
		t.Fatal("a retry, incomplete reply, or different conversation must not reuse the chat")
	}
	e.recordWashChat(other, "")
	if _, ok := e.washChats[other]; ok {
		t.Fatal("an incomplete or failed reply must clear the session")
	}
	e.recordWashChat(first, url+"-new")
	if got := e.washChats[first]; got.count != 1 || got.url != url+"-new" {
		t.Fatalf("new conversation must restart the count: %+v", got)
	}
}
