package rename

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyInterruptedLeavesRecoveryInstructions(t *testing.T) {
	if os.Getenv("TULAI_RENAME_CRASH_HELPER") == "1" {
		dir := os.Getenv("TULAI_RENAME_CRASH_DIR")
		_ = apply([]Change{{From: filepath.Join(dir, "original.txt"), To: filepath.Join(dir, "new.txt")}}, func(from, to string) error {
			if err := renameNoReplace(from, to); err != nil {
				os.Exit(8)
			}
			os.Exit(7)
			return nil
		})
		os.Exit(9)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "original.txt"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplyInterruptedLeavesRecoveryInstructions$")
	cmd.Env = append(os.Environ(), "TULAI_RENAME_CRASH_HELPER=1", "TULAI_RENAME_CRASH_DIR="+dir)
	var exitErr *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("helper = %v", err)
	}
	tempDirs, err := filepath.Glob(filepath.Join(dir, ".tulai-rename-*"))
	if err != nil || len(tempDirs) != 1 {
		t.Fatalf("temporary directory = %v, %v", tempDirs, err)
	}
	got, err := os.ReadFile(filepath.Join(tempDirs[0], "files", "original.txt"))
	if err != nil || string(got) != "safe" {
		t.Fatalf("staged file = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(tempDirs[0], "恢复说明.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyInterruptedAfterPartialCommitKeepsMapping(t *testing.T) {
	if os.Getenv("TULAI_RENAME_PARTIAL_HELPER") == "1" {
		dir := os.Getenv("TULAI_RENAME_CRASH_DIR")
		calls := 0
		_ = apply([]Change{
			{From: filepath.Join(dir, "a"), To: filepath.Join(dir, "x")},
			{From: filepath.Join(dir, "b"), To: filepath.Join(dir, "y")},
		}, func(from, to string) error {
			if err := renameNoReplace(from, to); err != nil {
				os.Exit(8)
			}
			calls++
			if calls == 3 {
				os.Exit(7)
			}
			return nil
		})
		os.Exit(9)
	}
	dir := t.TempDir()
	for path, value := range map[string]string{"a": "A", "b": "B"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplyInterruptedAfterPartialCommitKeepsMapping$")
	cmd.Env = append(os.Environ(), "TULAI_RENAME_PARTIAL_HELPER=1", "TULAI_RENAME_CRASH_DIR="+dir)
	var exitErr *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("helper = %v", err)
	}
	tempDirs, err := filepath.Glob(filepath.Join(dir, ".tulai-rename-*"))
	if err != nil || len(tempDirs) != 1 {
		t.Fatalf("temporary directory = %v, %v", tempDirs, err)
	}
	guide, err := os.ReadFile(filepath.Join(tempDirs[0], "恢复说明.txt"))
	if err != nil || !strings.Contains(string(guide), `"a" -> "x"`) || !strings.Contains(string(guide), `"b" -> "y"`) {
		t.Fatalf("missing mapping: %s, %v", guide, err)
	}
	for path, want := range map[string]string{filepath.Join(dir, "x"): "A", filepath.Join(tempDirs[0], "files", "b"): "B"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
}

func TestApplyRestoresAllFilesWhenCommitFails(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for path, value := range map[string]string{a: "A", b: "B"} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	err := apply([]Change{{From: a, To: b}, {From: b, To: a}}, func(from, to string) error {
		calls++
		if calls == 4 {
			return errors.New("simulated failure")
		}
		return renameNoReplace(from, to)
	})
	if err == nil {
		t.Fatal("expected a commit failure")
	}
	for path, want := range map[string]string{a: "A", b: "B"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary files left behind: %v, %v", entries, err)
	}
}

func TestApplyRetainsTemporaryFilesWhenRollbackFails(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for path, value := range map[string]string{a: "A", b: "B"} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	err := apply([]Change{{From: a, To: b}, {From: b, To: a}}, func(from, to string) error {
		calls++
		if calls >= 4 {
			return errors.New("simulated failure")
		}
		return renameNoReplace(from, to)
	})
	if err == nil || !strings.Contains(err.Error(), "请手动恢复") {
		t.Fatalf("error = %v", err)
	}
	tempDirs, err := filepath.Glob(filepath.Join(dir, ".tulai-rename-*"))
	if err != nil || len(tempDirs) != 1 {
		t.Fatalf("temporary directory = %v, %v", tempDirs, err)
	}
	got, err := os.ReadFile(filepath.Join(tempDirs[0], "files", "b"))
	if err != nil || string(got) != "B" {
		t.Fatalf("staged file = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(tempDirs[0], "恢复说明.txt")); err != nil {
		t.Fatalf("missing recovery instructions: %v", err)
	}
	got, err = os.ReadFile(b)
	if err != nil || string(got) != "A" {
		t.Fatalf("committed file = %q, %v", got, err)
	}
}

func TestRollbackDoesNotMoveAReplacement(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	c, d := filepath.Join(dir, "c"), filepath.Join(dir, "d")
	saved := filepath.Join(dir, "saved")
	if err := os.WriteFile(a, []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("C"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := apply([]Change{{From: a, To: b}, {From: c, To: d}}, func(from, to string) error {
		calls++
		if calls == 4 {
			if err := os.Rename(b, saved); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(b, []byte("replacement"), 0o600); err != nil {
				t.Fatal(err)
			}
			return errors.New("simulated failure")
		}
		return renameNoReplace(from, to)
	})
	if err == nil || !strings.Contains(err.Error(), "目标已变化") {
		t.Fatalf("error = %v", err)
	}
	for path, want := range map[string]string{b: "replacement", saved: "A", c: "C"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
	if _, err := os.Stat(a); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement moved to original path: %v", err)
	}
}

func TestRollbackFailureKeepsMappingWhenTemporaryFilesAreEmpty(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	c, d := filepath.Join(dir, "c"), filepath.Join(dir, "d")
	if err := os.WriteFile(a, []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("C"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := apply([]Change{{From: a, To: b}, {From: c, To: d}}, func(from, to string) error {
		calls++
		if calls == 4 || calls == 5 {
			return errors.New("simulated failure")
		}
		return renameNoReplace(from, to)
	})
	if err == nil || !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
		t.Fatalf("error = %v", err)
	}
	dirs, err := filepath.Glob(filepath.Join(dir, ".tulai-rename-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("recovery directory = %v, %v", dirs, err)
	}
	guide, err := os.ReadFile(filepath.Join(dirs[0], "恢复说明.txt"))
	if err != nil || !strings.Contains(string(guide), `"a" -> "b"`) {
		t.Fatalf("recovery mapping = %s, %v", guide, err)
	}
	got, err := os.ReadFile(b)
	if err != nil || string(got) != "A" {
		t.Fatalf("unrestored file = %q, %v", got, err)
	}
}

func TestApplySwapsAndUndo(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for path, data := range map[string]string{a: "A", b: "B"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	changes := []Change{{From: a, To: b}, {From: b, To: a}}
	for pass := 0; pass < 2; pass++ {
		if err := Apply(changes); err != nil {
			t.Fatal(err)
		}
		gotA, errA := os.ReadFile(a)
		gotB, errB := os.ReadFile(b)
		wantA, wantB := "B", "A"
		if pass == 1 {
			wantA, wantB = "A", "B"
		}
		if errA != nil || errB != nil || string(gotA) != wantA || string(gotB) != wantB {
			t.Fatalf("pass %d: a=%q (%v), b=%q (%v)", pass, gotA, errA, gotB, errB)
		}
	}
}

func TestApplyRejectsCollisionsAndNestedSelections(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Apply([]Change{{From: a, To: b}}); err == nil {
		t.Fatal("existing destination was accepted")
	}
	if err := Apply([]Change{{From: a, To: filepath.Join(dir, "c")}, {From: b, To: filepath.Join(dir, "c")}}); err == nil {
		t.Fatal("duplicate destinations were accepted")
	}
	if err := Apply([]Change{{From: a, To: filepath.Join(dir, "..", "outside")}}); err == nil {
		t.Fatal("destination outside the source directory was accepted")
	}
	child := filepath.Join(dir, "folder")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(child, "inner")
	if err := os.WriteFile(inner, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply([]Change{{From: child, To: filepath.Join(dir, "other")}, {From: inner, To: filepath.Join(child, "new")}}); err == nil || !strings.Contains(err.Error(), "内部") {
		t.Fatalf("nested selection: %v", err)
	}
	for path, want := range map[string]string{a: "a.txt", b: "b.txt", inner: "safe"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s changed: %q, %v", path, got, err)
		}
	}
}

func TestRenameNoReplaceDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = os.WriteFile(a, []byte("A"), 0o600)
	_ = os.WriteFile(b, []byte("B"), 0o600)
	if err := renameNoReplace(a, b); err == nil {
		t.Fatal("replaced an existing destination")
	}
	got, err := os.ReadFile(b)
	if err != nil || string(got) != "B" {
		t.Fatalf("destination changed: %q, %v", got, err)
	}
}

func TestApplyUnicodeEquivalentName(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "é.txt"), filepath.Join(dir, "e\u0301.txt")
	if err := os.WriteFile(from, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err1 := os.Lstat(from)
	second, err2 := os.Lstat(to)
	if err1 != nil || err2 != nil || !os.SameFile(first, second) {
		t.Skip("filesystem keeps these Unicode names distinct")
	}
	if err := Apply([]Change{{From: from, To: to}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(to)
	if err != nil || string(got) != "safe" {
		t.Fatalf("renamed file = %q, %v", got, err)
	}
}
