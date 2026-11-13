package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
)

func main() {
	output := flag.String("out", "internal/camoufox/bundled/windows-amd64.zip", "output bundle path")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	last := ""
	err := camoufox.PrepareWindowsRuntimeBundle(ctx, *output, func(progress camoufox.Progress) {
		if progress.Text != "" && progress.Text != last {
			last = progress.Text
			fmt.Println(progress.Text)
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "构建失败:", err)
		os.Exit(1)
	}
	fmt.Println("已生成", *output)
}
