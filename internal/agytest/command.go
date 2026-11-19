package agytest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func Executable(t testing.TB) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func RunCommand() {
	if len(os.Args) < 3 || os.Args[1] != "-p" {
		return
	}
	home := os.Getenv("HOME")
	if home == "" {
		fmt.Fprintln(os.Stderr, "fixture requires HOME")
		os.Exit(1)
	}
	cwd, err := os.Getwd()
	if err == nil {
		if os.Args[2] == "/usage" {
			err = usage(home, cwd)
		} else {
			err = generate(home, cwd, os.Args[2])
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func record(dir string, files map[string]string) error {
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func mode(home, name string) string {
	data, _ := os.ReadFile(filepath.Join(home, name))
	return strings.TrimSpace(string(data))
}

func usage(home, cwd string) error {
	if err := record(home, map[string]string{
		"seen-cwd.txt": cwd, "seen-home.txt": home,
		"seen-proxy.txt": os.Getenv("HTTP_PROXY"), "seen-args.txt": strings.Join(os.Args[1:], " "),
	}); err != nil {
		return err
	}
	switch mode(home, "mode.txt") {
	case "auth":
		fmt.Println("Authentication required. Please visit the URL")
		time.Sleep(30 * time.Second)
		return record(home, map[string]string{"alive.txt": "still running"})
	case "refresh-auth":
		data, err := os.ReadFile(filepath.Join(home, "refresh.json"))
		if err != nil {
			return err
		}
		if err := os.WriteFile(os.Getenv("TULAI_USAGE_TEST_KEYRING"), data, 0o600); err != nil {
			return err
		}
		fmt.Println("Authentication required")
		time.Sleep(30 * time.Second)
		return nil
	case "stdin":
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			line = "none"
		}
		if err := record(home, map[string]string{"stdin.txt": strings.TrimSuffix(line, "\n")}); err != nil {
			return err
		}
		fmt.Println("Authentication required")
		return nil
	case "sleep":
		time.Sleep(30 * time.Second)
		return nil
	case "bad":
		fmt.Println("not-json secret-should-not-leak")
		return errors.New("fixture failed")
	case "failed-quota":
		if err := printQuota(home); err != nil {
			return err
		}
		return errors.New("secret-should-not-leak")
	default:
		return printQuota(home)
	}
}

func printQuota(home string) error {
	data, err := os.ReadFile(filepath.Join(home, "usage.json"))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func generate(home, cwd, prompt string) error {
	if err := record(home, map[string]string{"last-cwd.txt": cwd, "last-prompt.txt": prompt}); err != nil {
		return err
	}
	if err := stageReference(home, cwd, prompt); err != nil {
		return err
	}
	root := filepath.Join(home, ".gemini", "antigravity-cli")
	fixture := os.Getenv("AGY_FIXTURE")
	if fixture == "" {
		fixture = mode(home, "fixture.txt")
	}
	switch fixture {
	case "quota":
		fmt.Println(`{"event":"init","conversation_id":"conv-quota"}`)
		fmt.Println(`{"event":"error","error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota exceeded"}}`)
	case "quota-reset":
		fmt.Println(`{"event":"error","error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"rate limit","retryDelay":"3600s"}}`)
	case "unauth", "hang", "login", "login-keyring":
		return login(root, home, fixture)
	case "deny":
		fmt.Println(`{"event":"init","conversation_id":"conv-deny"}`)
		fmt.Println("user denied permission for read_file(/workspace/agy-test/poster.png)")
		fmt.Println(`{"event":"result","result":{"status":"SUCCESS","response":""}}`)
		return nil
	case "fail":
	default:
		return writeImage(root, home)
	}
	fmt.Println(`{"event":"result","result":{"status":"ERROR","response":""}}`)
	return nil
}

func stageReference(home, cwd, prompt string) error {
	_, tail, found := strings.Cut(prompt, "edit the reference image ")
	if !found {
		return nil
	}
	ref, _, _ := strings.Cut(tail, ": ")
	relative, err := filepath.Rel(cwd, ref)
	if err != nil || !filepath.IsLocal(relative) {
		return nil
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "staged-ref"), data, 0o600)
}

func login(root, home, fixture string) error {
	fmt.Println("Authentication required. Please visit the URL to log in:")
	fmt.Println("https://accounts.google.com/o/oauth2/auth?client_id=agy")
	fmt.Println("Or, paste the authorization code here and press Enter:")
	switch fixture {
	case "login-keyring":
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		if err := record(home, map[string]string{"keyring-ready": ""}); err != nil {
			return err
		}
		fmt.Println("OAuth: authenticated successfully as keyring@example.com")
		time.Sleep(30 * time.Second)
	case "login":
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		if err := os.MkdirAll(root, 0o700); err != nil {
			return err
		}
		return record(root, map[string]string{
			"antigravity-oauth-token": `{"token":{"access_token":"login-access","refresh_token":"login-refresh","token_type":"Bearer"},"auth_method":"consumer"}`,
			"cli.log":                 "OAuth: authenticated successfully as someone@gmail.com",
		})
	default:
		time.Sleep(30 * time.Second)
	}
	return nil
}

func writeImage(root, home string) error {
	folder := filepath.Join(root, "brain", "conv-test")
	junk := filepath.Join(folder, ".tempmediaStorage")
	if err := os.MkdirAll(junk, 0o700); err != nil {
		return err
	}
	if err := record(junk, map[string]string{"junk.jpg": "skip"}); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(home, "sample.jpg"))
	if os.IsNotExist(err) {
		data = []byte("x")
	} else if err != nil {
		return err
	}
	output := filepath.Join(folder, "image_1.jpg")
	if err := os.WriteFile(output, data, 0o600); err != nil {
		return err
	}
	response, err := json.Marshal(output + "\n")
	if err != nil {
		return err
	}
	fmt.Println(`{"event":"init","conversation_id":"conv-test"}`)
	fmt.Printf("{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":%s}}\n", response)
	return nil
}
