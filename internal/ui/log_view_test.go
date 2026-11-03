package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
)

func TestRunLogShowsTimeAndLevelInColumns(t *testing.T) {
	previous := logging.Log
	logging.Log = logging.New(10)
	defer func() { logging.Log = previous }()
	logging.Log.Info("任务开始")
	logging.Log.Success("图片已保存")
	logging.Log.Warning("等待重试")
	logging.Log.Errorf("启动失败")
	logging.Log.Debugf("调试内容")
	stamp := logging.Log.Entries()[0].Time.Local().Format("15:04:05")

	for _, fill := range []bool{false, true} {
		a := &App{}
		tt := ui.NewTester(func(c *ui.Context) {
			a.look = applyWorkbenchTheme(c)
			a.runLog(c, fill)
		}, 320, 240)
		tt.Advance(100 * time.Millisecond)
		if !tt.HasText(stamp) || tt.HasText("调试内容") {
			t.Fatalf("fill=%v: timestamp missing or debug shown: %v", fill, tt.Texts())
		}
		when, _ := tt.Find(stamp)
		firstLevel, _ := tt.Find("信息")
		if when.X+when.W > firstLevel.X || when.Y+when.H < firstLevel.Y || firstLevel.Y+firstLevel.H < when.Y {
			t.Fatalf("fill=%v: timestamp and level are not aligned: %v %v", fill, when, firstLevel)
		}
		for _, pair := range [][2]string{{"信息", "任务开始"}, {"成功", "图片已保存"}, {"警告", "等待重试"}, {"错误", "启动失败"}} {
			level, ok := tt.Find(pair[0])
			if !ok {
				t.Fatalf("fill=%v: missing level %q", fill, pair[0])
			}
			message, ok := tt.Find(pair[1])
			if !ok || message.X <= level.X+level.W || message.Y+message.H < level.Y || level.Y+level.H < message.Y {
				t.Fatalf("fill=%v: level %q and message %q are not aligned: %v %v", fill, pair[0], pair[1], level, message)
			}
		}
	}
}
