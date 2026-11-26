package strategy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/fingerprint"
	"github.com/xxww0098/tulai/internal/fingerprinttest"
)

const fixtureBrowserPort = 40001

var fixtureBrowser struct {
	sync.Mutex
	root    string
	process *fingerprint.Process
}

func TestMain(tests *testing.M) {
	result := tests.Run()
	if fixtureBrowser.process != nil {
		fixtureBrowser.process.Close()
		fingerprint.Unregister(fixtureBrowserPort, fixtureBrowser.process)
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
	dataDir := fingerprinttest.ConfigDir(t)
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
		ctx, cancel := context.WithTimeout(t.Context(), fingerprinttest.StartTimeout)
		defer cancel()
		process, err := fingerprint.Start(ctx, dataDir, fingerprint.Options{
			Profile: filepath.Join(fixtureBrowser.root, "profile"), Headless: true, DirectNetwork: true,
		}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		fixtureBrowser.process = process
		fingerprint.Register(fixtureBrowserPort, process)
	}
	client, err := cdp.NewClient(fingerprint.Endpoint(fixtureBrowserPort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}
