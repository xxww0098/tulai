// fingerprint-smoke installs the managed Fingerprint runtime and verifies its private page bridge.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/fingerprint"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "失败:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	lastNote := ""
	if _, err := browser.EnsureRuntime(ctx, browser.ConfigDir(), browser.KindFingerprint, func(p fingerprint.Progress) {
		if p.Text != "" && p.Text != lastNote {
			lastNote = p.Text
			fmt.Println(p.Text)
		}
	}); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "tulai-fingerprint-smoke-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	proc, err := fingerprint.Start(ctx, browser.ConfigDir(), fingerprint.Options{Profile: dir, Headless: true}, os.Stderr)
	if err != nil {
		return err
	}
	defer proc.Close()
	fingerprint.Register(9555, proc)
	defer fingerprint.Unregister(9555, proc)
	client, err := cdp.NewClient(fingerprint.Endpoint(9555))
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession("about:blank", nil)
	if err != nil {
		return err
	}
	defer session.Close()
	if got, err := session.Evaluate(`1+1`); err != nil || got != "2" {
		return fmt.Errorf("页面求值: %s %v", got, err)
	}
	session.Handle("ping", func(data string) string { return `{"echo":` + data + `}` })
	if got, err := session.EvaluateAsync(`tulaiCall('ping','42')`); err != nil || got != `{"echo":42}` {
		return fmt.Errorf("页面桥: %s %v", got, err)
	}
	png, err := session.CaptureScreenshotRect(0, 0, 200, 100)
	if err != nil || len(png) == 0 {
		return fmt.Errorf("截图: %d bytes %v", len(png), err)
	}
	if _, err := session.Evaluate(`document.body.innerHTML='<textarea id="text"></textarea>'`); err != nil {
		return err
	}
	if err := session.InsertText("#text", "hello\nFingerprint"); err != nil {
		return err
	}
	if got, err := session.Evaluate(`document.querySelector('#text').value`); err != nil || got != `"hello\nFingerprint"` {
		return fmt.Errorf("可信输入: %s %v", got, err)
	}
	fmt.Println("Fingerprint 冒烟通过：页面求值、页面桥、截图、可信输入")
	return nil
}
