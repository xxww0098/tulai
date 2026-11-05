package agy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CredentialStore accesses agy's shared system credential, never a browser keychain.
type CredentialStore interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
}

// Keyring is shared by every CLI operation. Tests with a fake CLI use nil.
var Keyring CredentialStore = systemKeyring{}

// shortcut: 只协调本进程的切号；支持多个 tulai 进程时改用跨进程锁。
var cliGate = make(chan struct{}, 1)

var errCredential = errors.New("OAuth 凭据无效，请重新登录")

const emptyCredential = `{"token":{},"auth_method":"consumer"}`

func lockCLI(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case cliGate <- struct{}{}:
		return func() { <-cliGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func normalizeCredential(data []byte) ([]byte, error) {
	data = bytes.TrimSpace(data)
	if bytes.HasPrefix(data, []byte("go-keyring-base64:")) {
		var err error
		data, err = base64.StdEncoding.DecodeString(string(bytes.TrimPrefix(data, []byte("go-keyring-base64:"))))
		if err != nil {
			return nil, errCredential
		}
	}
	var payload struct {
		Token struct {
			RefreshToken string `json:"refresh_token"`
		} `json:"token"`
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &payload) != nil || strings.TrimSpace(payload.Token.RefreshToken) == "" {
		return nil, errCredential
	}
	return data, nil
}

func credentialEmail(data []byte) string {
	var payload struct {
		IDToken string `json:"id_token"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	parts := strings.Split(payload.IDToken, ".")
	if len(parts) != 3 {
		return ""
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	var info struct {
		Email string `json:"email"`
	}
	// This unverified claim is only an account label; agy validates the OAuth token.
	if err != nil || json.Unmarshal(claims, &info) != nil || !strings.Contains(info.Email, "@") {
		return ""
	}
	return strings.TrimSpace(info.Email)
}

func credentialSourceEmail(data []byte, home string) string {
	if email := credentialEmail(data); email != "" {
		return email
	}
	file, _ := os.ReadFile(TokenPath(home))
	var source, saved struct {
		Token struct {
			RefreshToken string `json:"refresh_token"`
		} `json:"token"`
	}
	if json.Unmarshal(data, &source) == nil && json.Unmarshal(file, &saved) == nil && source.Token.RefreshToken != "" && source.Token.RefreshToken == saved.Token.RefreshToken {
		return Email(home)
	}
	return ""
}

func writeCredential(home string, data []byte) error {
	path := TokenPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.New("无法创建 OAuth 凭据目录")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".oauth-*")
	if err != nil {
		return errors.New("无法保存 OAuth 凭据")
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return errors.New("无法保存 OAuth 凭据")
	}
	return nil
}

type cliSession struct {
	home     string
	initial  []byte
	previous []byte
	active   []byte
	keyring  CredentialStore
	unlock   func()
}

// beginCLI holds the credential switch until the subprocess and token save finish.
func beginCLI(ctx context.Context, home string, login bool) (*cliSession, error) {
	unlock, err := lockCLI(ctx)
	if err != nil {
		return nil, err
	}
	s := &cliSession{home: home, keyring: Keyring, unlock: unlock}
	s.initial, err = os.ReadFile(TokenPath(home))
	if err != nil && !os.IsNotExist(err) {
		unlock()
		return nil, errors.New("无法读取 OAuth 凭据")
	}
	if login {
		s.active = []byte(emptyCredential)
	} else {
		s.active, err = normalizeCredential(s.initial)
		if err != nil {
			unlock()
			return nil, ErrNeedLogin
		}
	}
	if s.keyring != nil {
		keyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		s.previous, err = s.keyring.Read(keyCtx)
		if err != nil {
			unlock()
			return nil, err
		}
		if err := s.keyring.Write(keyCtx, s.active); err != nil {
			err = errors.Join(err, s.restore())
			unlock()
			return nil, err
		}
	}
	if err = writeCredential(home, s.active); err != nil {
		err = errors.Join(err, s.restore())
		unlock()
		return nil, err
	}
	return s, nil
}

func (s *cliSession) current(ctx context.Context) ([]byte, error) {
	var system []byte
	if s.keyring != nil {
		data, err := s.keyring.Read(ctx)
		if err != nil {
			return nil, err
		}
		system, _ = normalizeCredential(data)
	}
	file, _ := os.ReadFile(TokenPath(s.home))
	file, _ = normalizeCredential(file)
	if len(system) > 0 && !bytes.Equal(system, s.active) {
		return system, nil
	}
	if len(file) > 0 {
		return file, nil
	}
	if len(system) > 0 {
		return system, nil
	}
	return nil, errCredential
}

func (s *cliSession) save() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data, err := s.current(ctx)
	if err != nil {
		return err
	}
	if err := writeCredential(s.home, data); err != nil {
		return err
	}
	if s.keyring != nil {
		if err := s.keyring.Write(ctx, data); err != nil {
			return err
		}
	}
	return nil
}

func (s *cliSession) restore() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var err error
	if s.keyring != nil {
		data := s.previous
		if len(data) == 0 {
			data = []byte(emptyCredential)
		}
		err = s.keyring.Write(ctx, data)
	}
	if s.initial != nil {
		return errors.Join(err, writeCredential(s.home, s.initial))
	}
	if removeErr := os.Remove(TokenPath(s.home)); !os.IsNotExist(removeErr) {
		err = errors.Join(err, removeErr)
	}
	return err
}
