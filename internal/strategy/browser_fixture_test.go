package strategy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/camoufoxtest"
	"github.com/xxww0098/tulai/internal/cdp"
)

const fixtureBrowserPort = 40001

var fixtureBrowser struct {
	sync.Mutex
	root    string
	process *camoufox.Process
}

func TestMain(tests *testing.M) {
	result := tests.Run()
	if fixtureBrowser.process != nil {
		fixtureBrowser.process.Close()
		camoufox.Unregister(fixtureBrowserPort, fixtureBrowser.process)
	}
	if fixtureBrowser.root != "" {
		if err := os.RemoveAll(fixtureBrowser.root); err != nil {
			fmt.Fprintln(os.Stderr, "remove strategy fixture profile:", err)
			result = 1
		}
	}
	os.Exit(result)
}

func newFixtureClient(t *testing.T) *cdp.Client {
	t.Helper()
	dataDir := camoufoxtest.ConfigDir(t)
	fixtureBrowser.Lock()
	defer fixtureBrowser.Unlock()
	if fixtureBrowser.process == nil {
		if fixtureBrowser.root == "" {
			root, err := os.MkdirTemp("", "tulai-strategy-fixture-")
			if err != nil {
				t.Fatal(err)
			}
			fixtureBrowser.root = root
		}
		ctx, cancel := context.WithTimeout(t.Context(), camoufoxtest.StartTimeout)
		defer cancel()
		process, err := camoufox.Start(ctx, dataDir, camoufox.Options{
			Profile: filepath.Join(fixtureBrowser.root, "profile"), Headless: true, DirectNetwork: true,
		}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		fixtureBrowser.process = process
		camoufox.Register(fixtureBrowserPort, process)
	}
	client, err := cdp.NewClient(camoufox.Endpoint(fixtureBrowserPort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}
