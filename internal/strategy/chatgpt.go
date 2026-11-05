package strategy

import (
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/chatgpt"
)

// ChatGPT supplies the account page; its image protocol is executed by tasks in Go.
type ChatGPT struct{ base }

func (ChatGPT) Platform() string               { return chatgpt.Platform }
func (ChatGPT) HomeURL() string                { return chatgpt.HomeURL }
func (ChatGPT) InstallHook(*cdp.Session) error { return nil }
