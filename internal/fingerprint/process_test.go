package fingerprint

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/realbrowser/fingerprintkernel/profile"
)

func TestRuntimeEnvIsolatesFingerprintConfiguration(t *testing.T) {
	for _, key := range []string{"TULAI_FINGERPRINT_MIRROR", "tulai_fingerprint_url", "FONTCONFIG_FILE", "NAPI_RS_NATIVE_LIBRARY_PATH", "NAPI_RS_ENFORCE_VERSION_CHECK", "NAPI_RS_FORCE_WASI"} {
		t.Setenv(key, "foreign")
	}
	t.Setenv("TULAI_RUNTIME_ENV_TEST", "preserved")
	got := map[string]string{}
	for _, item := range runtimeEnv("", t.TempDir()) {
		key, value, _ := strings.Cut(item, "=")
		got[strings.ToUpper(key)] = value
	}
	for _, key := range []string{"TULAI_FINGERPRINT_MIRROR", "TULAI_FINGERPRINT_URL", "FONTCONFIG_FILE", "NAPI_RS_NATIVE_LIBRARY_PATH", "NAPI_RS_ENFORCE_VERSION_CHECK", "NAPI_RS_FORCE_WASI"} {
		if _, ok := got[key]; ok {
			t.Fatalf("inherited fingerprint setting %s", key)
		}
	}
	if got["TULAI_RUNTIME_ENV_TEST"] != "preserved" {
		t.Fatal("unrelated environment was removed")
	}
}

func TestBrowserRuntimeEnvDirectNetwork(t *testing.T) {
	proxyVars := []string{"HTTP_PROXY", "https_proxy", "ALL_PROXY", "npm_config_https_proxy"}
	for _, key := range proxyVars {
		t.Setenv(key, "http://127.0.0.1:65535")
	}
	t.Setenv("TULAI_RUNTIME_ENV_TEST", "preserved")

	dir := t.TempDir()
	for _, direct := range []bool{true, false} {
		values := make(map[string]string)
		for _, item := range browserRuntimeEnv(dir, direct) {
			key, value, _ := strings.Cut(item, "=")
			values[strings.ToUpper(key)] = value
		}
		for _, key := range proxyVars {
			if _, present := values[strings.ToUpper(key)]; present == direct {
				t.Errorf("direct=%v: %s present=%v", direct, key, present)
			}
		}
		if values["TULAI_RUNTIME_ENV_TEST"] != "preserved" {
			t.Errorf("direct=%v: unrelated environment was removed", direct)
		}
	}
}

func TestCallReplyCancellationAndExit(t *testing.T) {
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	p := &Process{in: inputWriter, ctx: life, cancel: cancel, pending: map[int64]chan reply{}, listeners: map[int64]func(Event){}}
	t.Cleanup(func() { inputReader.Close(); inputWriter.Close(); outputReader.Close(); outputWriter.Close() })
	requests := make(chan map[string]any, 4)
	go func() {
		scanner := bufio.NewScanner(inputReader)
		for scanner.Scan() {
			var r map[string]any
			if json.Unmarshal(scanner.Bytes(), &r) == nil {
				requests <- r
			}
		}
	}()
	go p.read(outputReader)
	result := make(chan error, 1)
	var got struct{ Value int }
	go func() { result <- p.Call(context.Background(), "page", "ping", nil, &got) }()
	request := <-requests
	data, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]int{"value": 42}})
	_, _ = outputWriter.Write(append(data, '\n'))
	if err := <-result; err != nil || got.Value != 42 {
		t.Fatalf("reply: %+v %v", got, err)
	}
	ctx, stop := context.WithCancel(context.Background())
	go func() { result <- p.Call(ctx, "page", "wait", nil, nil) }()
	<-requests
	stop()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if request := <-requests; request["method"] != "cancel" {
		t.Fatal(request)
	}
	go func() { result <- p.Call(context.Background(), "page", "ping", nil, nil) }()
	<-requests
	outputWriter.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("EOF succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("exit left a pending command")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pending) != 0 {
		t.Fatal("pending calls leaked")
	}
}

func TestStripANSIErrorFormatting(t *testing.T) {
	got := stripANSI("\x1b[2mBrowserContext.new_page: \x1b[22mTarget page, context or browser has been closed\n")
	want := "BrowserContext.new_page: Target page, context or browser has been closed\n"
	if got != want {
		t.Fatalf("stripANSI=%q want %q", got, want)
	}
}

func TestApplyProfileIdentityUsesFingerprintKernel(t *testing.T) {
	root := t.TempDir()
	profileA := filepath.Join(root, "account-a")
	profileB := filepath.Join(root, "account-b")

	// Pre-saved user preferences must survive random fingerprint generation.
	if err := profile.WriteProfile(profileA, profile.FingerprintSpec{
		Lang:         "zh-CN",
		Timezone:     "Asia/Shanghai",
		WindowWidth:  1200,
		WindowHeight: 800,
		ColorScheme:  profile.ColorSchemeDark,
	}); err != nil {
		t.Fatal(err)
	}

	optsA := Options{Profile: profileA}
	if err := applyProfileIdentity(t.Context(), profileA, []string{root}, &optsA); err != nil {
		t.Fatal(err)
	}
	if optsA.Identity == nil || optsA.Identity.Seed == 0 || optsA.Identity.Brand != profile.BrandChrome {
		t.Fatalf("missing generated identity: %+v", optsA.Identity)
	}
	if optsA.Lang != "zh-CN" || optsA.Timezone != "Asia/Shanghai" || optsA.WindowWidth != 1200 || optsA.WindowHeight != 800 || optsA.ColorScheme != profile.ColorSchemeDark {
		t.Fatalf("saved preferences lost: %+v", optsA)
	}
	if _, err := os.Stat(filepath.Join(profileA, legacyIdentityFile)); !os.IsNotExist(err) {
		t.Fatalf("expected no new legacy identity file, got stat err=%v", err)
	}

	// Second call on the same profile reuses the exact persisted seed from .browserkernel.json.
	optsA2 := Options{Profile: profileA, Lang: "en-US"}
	if err := applyProfileIdentity(t.Context(), profileA, []string{root}, &optsA2); err != nil {
		t.Fatal(err)
	}
	if optsA2.Identity == nil || optsA2.Identity.Seed != optsA.Identity.Seed {
		t.Fatalf("seed drifted across reload: %d -> %+v", optsA.Identity.Seed, optsA2.Identity)
	}
	if optsA2.Lang != "en-US" {
		t.Fatalf("explicit option Lang overridden: %q", optsA2.Lang)
	}

	// Sibling profile avoids seed collisions through fingerprintkernel.
	optsB := Options{Profile: profileB}
	if err := applyProfileIdentity(t.Context(), profileB, []string{root}, &optsB); err != nil {
		t.Fatal(err)
	}
	if optsB.Identity == nil || optsB.Identity.Seed == 0 || optsB.Identity.Seed == optsA.Identity.Seed {
		t.Fatalf("sibling profile failed to get distinct seed: a=%+v b=%+v", optsA.Identity, optsB.Identity)
	}

	// Legacy .tulai-fingerprint.json migrates cleanly into .browserkernel.json.
	legacyDir := filepath.Join(root, "legacy-account")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyJSON := `{"version":1,"seed":200002,"platform":"macos","platformVersion":"15.3.1","brand":"Chrome","hardwareConcurrency":10,"screen":{"width":1512,"height":982,"availTop":37,"availBottom":70,"dpr":2,"windowWidth":1440,"windowHeight":860}}`
	if err := os.WriteFile(filepath.Join(legacyDir, legacyIdentityFile), []byte(legacyJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	optsLegacy := Options{Profile: legacyDir}
	if err := applyProfileIdentity(t.Context(), legacyDir, []string{root}, &optsLegacy); err != nil {
		t.Fatal(err)
	}
	if optsLegacy.Identity == nil || optsLegacy.Identity.Seed != 200002 || optsLegacy.Identity.PlatformVersion != "15.3.1" {
		t.Fatalf("legacy identity not migrated: %+v", optsLegacy.Identity)
	}
	persisted, err := profile.ReadProfile(legacyDir)
	if err != nil || persisted.Seed != 200002 {
		t.Fatalf("migrated identity not persisted in .browserkernel.json: %+v err=%v", persisted, err)
	}
}
