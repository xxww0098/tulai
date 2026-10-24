// ext-smoke 启动本机扩展桥，等待千问扩展 hello，并做一次 ping 与 status。
// 设置 XBS_EXT_LIVE=1 时，在已登录的千问页上再跑一次图生图。步骤见 extension/qianwen/README.md。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"xbs/internal/extbridge"
	"xbs/internal/tasks"
)

func main() {
	addr := os.Getenv("XBS_EXT_ADDR")
	if addr == "" {
		addr = extbridge.DefaultAddr
	}
	token := os.Getenv("XBS_EXT_TOKEN")
	if token == "" {
		token = "dev-token"
		fmt.Println("未设置 XBS_EXT_TOKEN，使用仅供本机开发的默认令牌 dev-token")
	}
	srv, err := extbridge.Listen(extbridge.Config{Addr: addr, Token: token})
	must(err)
	defer srv.Close()
	fmt.Printf("扩展桥监听 %s\n", srv.Addr())
	fmt.Printf("令牌 %s\n", token)
	fmt.Println("加载 extension/qianwen 后，在选项页保存同一令牌和端口。")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		sess, err := srv.Next(ctx)
		if err != nil {
			fmt.Println("退出")
			return
		}
		fmt.Printf("hello 已通过 session=%s platform=%s\n", sess.ID(), sess.Platform())
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if err := sess.Ping(callCtx); err != nil {
			fmt.Println("ping 失败:", err)
		} else {
			fmt.Println("ping/pong 成功")
		}
		cmd, err := extbridge.Command(extbridge.CmdStatus, nil)
		if err != nil {
			cancel()
			fmt.Println("status 命令:", err)
			continue
		}
		res, err := sess.Call(callCtx, cmd)
		cancel()
		if err != nil {
			fmt.Printf("status 失败: %v\n", err)
		} else {
			fmt.Printf("status 应答: %s\n", res.Payload)
		}
		if os.Getenv("XBS_EXT_LIVE") == "1" {
			if err := liveOnce(ctx, sess); err != nil {
				fmt.Println("实机生成失败:", err)
				os.Exit(1)
			}
			return
		}
		fmt.Println("等待扩展断开或下一次 hello。Ctrl+C 退出。")
		fmt.Println("要在已登录页面上跑一次图生图，另开进程并设置 XBS_EXT_LIVE=1、XBS_EXT_IMAGE、XBS_EXT_PROMPT。")
	}
}

func liveOnce(ctx context.Context, sess *extbridge.Session) error {
	image := os.Getenv("XBS_EXT_IMAGE")
	prompt := os.Getenv("XBS_EXT_PROMPT")
	if image == "" || prompt == "" {
		return fmt.Errorf("实机生成需要 XBS_EXT_IMAGE 和 XBS_EXT_PROMPT")
	}
	wait := 120 * time.Second
	if raw := os.Getenv("XBS_EXT_WAIT"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			return fmt.Errorf("XBS_EXT_WAIT 需要正整数秒")
		}
		wait = time.Duration(seconds) * time.Second
	}
	fmt.Println("开始一次实机图生图。请确认通义/千问页已经打开并登录。")
	out, err := tasks.RunQianwenExtension(ctx, sess, tasks.QianwenJob{
		TaskID:            "live",
		IdempotencyPrefix: "live",
		Prompt:            prompt,
		Images:            []string{image},
		Wait:              wait,
	})
	if out.Submitted {
		fmt.Println("发送已确认")
	}
	dir := os.Getenv("XBS_EXT_OUT")
	if dir == "" {
		dir = "ext-live-out"
	}
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return mkErr
	}
	for i, result := range out.Results {
		path := filepath.Join(dir, fmt.Sprintf("result-%02d.png", i+1))
		if werr := os.WriteFile(path, result.Data, 0o644); werr != nil {
			return werr
		}
		fmt.Printf("已保存 %s (%d 字节)\n", path, len(result.Data))
	}
	if err != nil {
		return err
	}
	if len(out.Results) == 0 {
		return fmt.Errorf("扩展没有返回图片")
	}
	return nil
}

func must(err error) {
	if err != nil {
		fmt.Println("失败:", err)
		os.Exit(1)
	}
}
