package strategy

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSendAcceptedNeedsBubbleOrConversation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, filepath.Join("js", "composer_send_test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("发送确认: %v\n%s", err, out)
	}
}
