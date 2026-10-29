package strategy

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLiveImagesWaitForDoubaoSignal(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, filepath.Join("js", "hook_live_test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("完成检测: %v\n%s", err, out)
	}
}
