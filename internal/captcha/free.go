package captcha

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"

	_ "golang.org/x/image/webp"
)

// FreeSolver 纯 CV 离线求解（对应原 free 后端）：
// 边缘灰度均值特征 + 余弦相似度。两种模式：
//   - 拖拽模式：九宫格 9 图 + 目标图 → 找最相似
//   - 拼图模式：九宫格乱序 → 全排列搜索最优拼接
type FreeSolver struct{ cfg Config }

// FreeSolver 构造器。
func NewFreeSolver(cfg Config) *FreeSolver { return &FreeSolver{cfg: cfg} }

// Name 后端名。
func (f *FreeSolver) Name() string { return "free" }

const featureDim = 20

// Solve 求解入口。
func (f *FreeSolver) Solve(ctx context.Context, req Request) Answer {
	gridBytes, err := decodeBase64(req.GridBase64)
	if err != nil {
		return Answer{Error: "九宫格图解码失败: " + err.Error()}
	}
	grid, _, err := image.Decode(bytes.NewReader(gridBytes))
	if err != nil {
		return Answer{Error: "九宫格图解码失败: " + err.Error()}
	}
	pieces := splitNineGrid(toGray(grid))
	if len(pieces) != 9 {
		return Answer{Error: fmt.Sprintf("九宫格切分异常: %d 块", len(pieces))}
	}
	if req.TargetBase6 != "" {
		return f.solveDrag(pieces, req.TargetBase6)
	}
	return f.solvePuzzle(pieces)
}

// solveDrag 拖拽模式：目标图四边特征拼接向量与每块比对。
func (f *FreeSolver) solveDrag(pieces [][][]float64, targetB64 string) Answer {
	tb, err := decodeBase64(targetB64)
	if err != nil {
		return Answer{Error: "目标图解码失败: " + err.Error()}
	}
	timg, _, err := image.Decode(bytes.NewReader(tb))
	if err != nil {
		return Answer{Error: "目标图解码失败: " + err.Error()}
	}
	target := concatEdges(toGray(timg))
	best, bestSim := 0, -2.0
	second := -2.0
	for i, p := range pieces {
		sim := cosine(concatEdges(p), target)
		if sim > bestSim {
			second, best, bestSim = bestSim, i, sim
		} else if sim > second {
			second = sim
		}
	}
	return Answer{
		Success:    true,
		Indices:    []int{best + 1},
		Confidence: clamp01(bestSim - second), // 首尾名差距作置信度
	}
}

// solvePuzzle 拼图模式：全排列搜索相邻边匹配分最高的排列。
func (f *FreeSolver) solvePuzzle(pieces [][][]float64) Answer {
	// 每块的四边特征
	feats := make([][4][]float64, 9) // 0上 1下 2左 3右
	for i, p := range pieces {
		feats[i] = [4][]float64{edgeFeature(p, 't'), edgeFeature(p, 'b'), edgeFeature(p, 'l'), edgeFeature(p, 'r')}
	}
	best := make([]int, 9)
	copy(best, []int{0, 1, 2, 3, 4, 5, 6, 7, 8})
	bestScore := -1e9
	order := make([]int, 9)
	var search func(idx int, used [9]bool, score float64)
	search = func(idx int, used [9]bool, score float64) {
		if score+40 < bestScore && idx > 2 {
			return // 剪枝：剩余满分也追不上
		}
		if idx == 9 {
			if score > bestScore {
				bestScore = score
				copy(best, order)
			}
			return
		}
		r, c := idx/3, idx%3
		for i := 0; i < 9; i++ {
			if used[i] {
				continue
			}
			used[i] = true
			order[idx] = i
			s := score
			if c > 0 {
				s += cosine(feats[order[idx-1]][3], feats[i][2])
			}
			if r > 0 {
				s += cosine(feats[order[idx-3]][1], feats[i][0])
			}
			search(idx+1, used, s)
			used[i] = false
		}
	}
	search(0, [9]bool{}, 0)
	// 返回第一格序号（拼图类通常拖首块到起点）
	return Answer{Success: true, Indices: []int{best[0] + 1}, Confidence: 0.5}
}

// toGray 转浮点灰度矩阵。
func toGray(img image.Image) [][]float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	g := make([][]float64, h)
	for y := 0; y < h; y++ {
		g[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			r, gr, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			g[y][x] = 0.299*float64(r>>8) + 0.587*float64(gr>>8) + 0.114*float64(bl>>8)
		}
	}
	return g
}

// splitNineGrid 切九块（左到右、上到下）。
func splitNineGrid(g [][]float64) [][][]float64 {
	h, w := len(g), len(g[0])
	gh, gw := h/3, w/3
	if gh == 0 || gw == 0 {
		return nil
	}
	var out [][][]float64
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			piece := make([][]float64, gh)
			for y := 0; y < gh; y++ {
				piece[y] = make([]float64, gw)
				copy(piece[y], g[r*gh+y][c*gw:c*gw+gw])
			}
			out = append(out, piece)
		}
	}
	return out
}

// edgeFeature 取指定边缘 dim 高/宽的灰度条带，沿边缘方向求均值 → 归一化向量。
func edgeFeature(g [][]float64, edge byte) []float64 {
	h, w := len(g), len(g[0])
	var stripe []float64
	switch edge {
	case 't':
		for x := 0; x < w; x++ {
			s, n := 0.0, 0
			for y := 0; y < minInt(featureDim, h); y++ {
				s += g[y][x]
				n++
			}
			stripe = append(stripe, s/float64(n))
		}
	case 'b':
		for x := 0; x < w; x++ {
			s, n := 0.0, 0
			for y := maxInt(0, h-featureDim); y < h; y++ {
				s += g[y][x]
				n++
			}
			stripe = append(stripe, s/float64(n))
		}
	case 'l':
		for y := 0; y < h; y++ {
			s, n := 0.0, 0
			for x := 0; x < minInt(featureDim, w); x++ {
				s += g[y][x]
				n++
			}
			stripe = append(stripe, s/float64(n))
		}
	default: // 'r'
		for y := 0; y < h; y++ {
			s, n := 0.0, 0
			for x := maxInt(0, w-featureDim); x < w; x++ {
				s += g[y][x]
				n++
			}
			stripe = append(stripe, s/float64(n))
		}
	}
	f := resizeVec(stripe, featureDim)
	norm2(f)
	return f
}

// concatEdges 四边特征拼接成一个向量（归一化）。
func concatEdges(g [][]float64) []float64 {
	var v []float64
	for _, e := range []byte{'t', 'b', 'l', 'r'} {
		v = append(v, edgeFeature(g, e)...)
	}
	norm2(v)
	return v
}

func cosine(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		if i < len(b) {
			s += a[i] * b[i]
		}
	}
	return s
}

func norm2(v []float64) {
	s := 0.0
	for _, x := range v {
		s += x * x
	}
	if s <= 0 {
		return
	}
	inv := 1 / sqrt(s)
	for i := range v {
		v[i] *= inv
	}
}

func resizeVec(v []float64, n int) []float64 {
	if len(v) == n {
		return v
	}
	out := make([]float64, n)
	if len(v) > n { // 简单降采样
		ratio := float64(len(v)) / float64(n)
		for i := 0; i < n; i++ {
			from, to := int(float64(i)*ratio), int(float64(i+1)*ratio)
			if to <= from {
				to = from + 1
			}
			s := 0.0
			for j := from; j < to && j < len(v); j++ {
				s += v[j]
			}
			out[i] = s / float64(to-from)
		}
	} else { // 边缘复制填充
		copy(out, v)
		for i := len(v); i < n; i++ {
			out[i] = v[len(v)-1]
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func sqrt(x float64) float64 { return math.Sqrt(x) }
