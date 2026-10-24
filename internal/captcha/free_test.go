package captcha

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// 构造 3x3 大图：每块纯色 + 块间渐变边缘；目标图取第 idx 块。
func buildGrid(t *testing.T, matchIdx int) (gridB64, targetB64 string) {
	t.Helper()
	const cell = 60
	img := image.NewRGBA(image.Rect(0, 0, cell*3, cell*3))
	colors := [9]color.RGBA{}
	base := color.RGBA{R: 200, A: 255}
	for i := range colors {
		// 每块一个不同的基础色，matchIdx 块在右边缘加特征条纹
		colors[i] = color.RGBA{R: uint8(30 + i*20), G: uint8(60 + i*10), B: uint8(90 + i*15), A: 255}
	}
	for r := 0; r < cell*3; r++ {
		for c := 0; c < cell*3; c++ {
			idx := (r/cell)*3 + c/cell
			col := colors[idx]
			img.Set(c, r, col)
		}
	}
	// 在 matchIdx 块加明显边缘特征（横条纹）
	mr, mc := matchIdx/3, matchIdx%3
	for r := 0; r < cell; r++ {
		for c := 0; c < cell; c++ {
			if r%6 < 3 {
				img.Set(mc*cell+c, mr*cell+r, base)
			}
		}
	}
	var gbuf bytes.Buffer
	if err := png.Encode(&gbuf, img); err != nil {
		t.Fatal(err)
	}
	// 目标图 = matchIdx 块的拷贝（带同样条纹）
	target := image.NewRGBA(image.Rect(0, 0, cell, cell))
	for r := 0; r < cell; r++ {
		for c := 0; c < cell; c++ {
			col := colors[matchIdx]
			if r%6 < 3 {
				col = base
			}
			target.Set(c, r, col)
		}
	}
	var tbuf bytes.Buffer
	if err := png.Encode(&tbuf, target); err != nil {
		t.Fatal(err)
	}
	return b64(gbuf.Bytes()), b64(tbuf.Bytes())
}

func b64(b []byte) string { return encodeB64ForTest(b) }

func TestFreeSolverDragMode(t *testing.T) {
	solver := NewFreeSolver(Config{Backend: "free"})
	for _, want := range []int{0, 4, 8} {
		grid, target := buildGrid(t, want)
		ans := solver.Solve(context.Background(), Request{Type: "grid", GridBase64: grid, TargetBase6: target})
		if !ans.Success {
			t.Fatalf("want=%d 求解失败: %s", want, ans.Error)
		}
		if len(ans.Indices) == 0 || ans.Indices[0] != want+1 {
			t.Errorf("want=%d got=%v", want+1, ans.Indices)
		}
	}
}

func TestFreeSolverPuzzleMode(t *testing.T) {
	solver := NewFreeSolver(Config{Backend: "free"})
	grid, _ := buildGrid(t, 4) // 无目标 → 拼图模式，至少要成功返回
	ans := solver.Solve(context.Background(), Request{Type: "grid", GridBase64: grid})
	if !ans.Success {
		t.Fatalf("拼图模式失败: %s", ans.Error)
	}
}

func TestParseJSONObject(t *testing.T) {
	cases := []string{
		`{"indices":[3],"confidence":0.9}`,
		"说明文字\n```json\n{\"indices\":[1,5],\"confidence\":0.8}\n```",
		`前缀 {"ratio": 0.42} 后缀`,
	}
	m, ok := parseJSONObject(cases[0])
	if !ok || len(m) != 2 {
		t.Errorf("case0: %v %v", ok, m)
	}
	m, ok = parseJSONObject(cases[1])
	if !ok {
		t.Fatal("case1 解析失败")
	}
	ans := answerFromModel(m)
	if len(ans.Indices) != 2 || ans.Indices[0] != 1 {
		t.Errorf("case1: %v", ans.Indices)
	}
	m, _ = parseJSONObject(cases[2])
	ans = answerFromModel(m)
	if ans.Ratio != 0.42 {
		t.Errorf("case2 ratio: %v", ans.Ratio)
	}
}
