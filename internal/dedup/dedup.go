// Package dedup 找出重复图片，把不重复的那份复制到新文件夹。
//
// 判重分两级，都不看文件名和元数据：
//   - 解码后像素的 SHA-256 相同，就是同一张图（换个编码、改个名也算）；
//   - 否则按 pHash（32×32 灰度做 DCT 取低频 8×8）的汉明距离判近似，差得越少越像。
//
// 分组用并查集，A 像 B、B 像 C 时三张归一组。组里留下分辨率最大的那张。
package dedup

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	_ "golang.org/x/image/webp"
)

// 报告文件名。和图片一起放在输出目录里，方便回头核对删了什么。
const ReportName = "重复报告.csv"

// 采样网格与低频块。32×32 进 DCT，取左上 8×8 的低频系数，去掉直流项后是 63 位。
const (
	sampleSide = 32
	lowFreq    = 8
)

// flatVariance 是「几乎没有纹理」的灰度方差门槛。纯色底、纯白底这种图在这里翻不过去。
const flatVariance = 4

// flatColorGap 是两张都平坦时允许的平均色差。纯白底和纯黑底差 255，必须挡住。
const flatColorGap = 48

// Signature 是一张图的判重指纹。
type Signature struct {
	Path   string
	Width  int
	Height int
	Bytes  int64
	// Pixels 是解码后 RGBA 字节的 SHA-256。同图不同编码时它相等，Hash 可能差几位。
	Pixels string
	// Hash 是 pHash：32×32 灰度做二维 DCT，取左上 8×8 低频按中位数取位，共 63 位。
	Hash uint64
	// MeanR/MeanG/MeanB 是缩放前的平均色，只用来挡住「纯白底 vs 纯黑底」这种平坦图。
	MeanR, MeanG, MeanB uint8
	// Flat 表示这张图几乎没有纹理（纯色、纯白底、纯黑底）。
	Flat bool
}

// Options 是一次去重的口径。
type Options struct {
	// MaxDistance 是汉明距离上限。0 只认像素完全一致的图。
	MaxDistance int
	// IgnoreFlatColor 关掉平坦图的配色护栏：纯白底和纯黑底也会判成同一张。
	IgnoreFlatColor bool
}

// Pixels 一张图的像素数，挑保留哪张时先比它。
func (s Signature) Pixels64() int64 { return int64(s.Width) * int64(s.Height) }

// Hash 算一张图的指纹。解不开的图返回错误，由调用方决定是跳过还是记下来。
func Hash(path string) (Signature, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Signature{}, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Signature{}, fmt.Errorf("解码 %s: %w", filepath.Base(path), err)
	}
	b := img.Bounds()
	sig := Signature{
		Path:   path,
		Width:  b.Dx(),
		Height: b.Dy(),
		Bytes:  int64(len(data)),
		Pixels: pixelDigest(img),
	}
	var gray [sampleSide * sampleSide]uint8
	sampleGray(img, gray[:])
	sig.MeanR, sig.MeanG, sig.MeanB = meanColor(img)
	sig.Flat = variance(gray[:]) < flatVariance
	sig.Hash = phash(gray[:])
	return sig, nil
}

// pixelDigest 把图画进固定 RGBA 缓冲再取摘要。用 RGBA() 取值，色板图和灰度图也能对齐。
func pixelDigest(img image.Image) string {
	b := img.Bounds()
	buf := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			buf = append(buf, byte(r>>8), byte(g>>8), byte(bl>>8), byte(a>>8))
		}
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// meanColor 是整图的平均色，按人眼对灰度的权重换算后比较用。
func meanColor(img image.Image) (uint8, uint8, uint8) {
	b := img.Bounds()
	var rs, gs, bs, n uint64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			rs += uint64(r >> 8)
			gs += uint64(g >> 8)
			bs += uint64(bl >> 8)
			n++
		}
	}
	if n == 0 {
		return 0, 0, 0
	}
	return uint8(rs / n), uint8(gs / n), uint8(bs / n)
}

// sampleGray 按面积平均把图缩到 32×32 灰度。区域平均比最近邻稳，缩放和压缩噪声不容易翻位。
func sampleGray(img image.Image, out []uint8) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	for gy := 0; gy < sampleSide; gy++ {
		y0 := b.Min.Y + gy*h/sampleSide
		y1 := b.Min.Y + (gy+1)*h/sampleSide
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for gx := 0; gx < sampleSide; gx++ {
			x0 := b.Min.X + gx*w/sampleSide
			x1 := b.Min.X + (gx+1)*w/sampleSide
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					c := color.Gray16Model.Convert(img.At(x, y)).(color.Gray16)
					sum += uint64(c.Y)
					n++
				}
			}
			if n == 0 {
				continue
			}
			out[gy*sampleSide+gx] = uint8(sum / n >> 8)
		}
	}
}

// variance 是灰度方差，用来判断这张图有没有内容。
func variance(gray []uint8) float64 {
	if len(gray) == 0 {
		return 0
	}
	var sum float64
	for _, v := range gray {
		sum += float64(v)
	}
	mean := sum / float64(len(gray))
	var acc float64
	for _, v := range gray {
		d := float64(v) - mean
		acc += d * d
	}
	return acc / float64(len(gray))
}

// cosTable 是 DCT 的余弦表，算一次就够。k 行 i 列是 cos(π(2i+1)k/2N)。
var cosTable = sync.OnceValue(func() [sampleSide][sampleSide]float64 {
	var t [sampleSide][sampleSide]float64
	for k := 0; k < sampleSide; k++ {
		for i := 0; i < sampleSide; i++ {
			t[k][i] = math.Cos(math.Pi * float64((2*i+1)*k) / float64(2*sampleSide))
		}
	}
	return t
})

// dct1d 就地做一维 DCT-II，用正交归一化系数。
func dct1d(v []float64) {
	tab := cosTable()
	out := make([]float64, len(v))
	for k := range v {
		var sum float64
		row := tab[k]
		for i := range v {
			sum += v[i] * row[i]
		}
		c := math.Sqrt(2.0 / float64(len(v)))
		if k == 0 {
			c = math.Sqrt(1.0 / float64(len(v)))
		}
		out[k] = c * sum
	}
	copy(v, out)
}

// phash 先对 32×32 灰度做二维 DCT，再取左上 8×8 的低频系数（不含直流项），
// 以中位数为界取 63 位。频域表示对整体亮度、对比度和压缩噪声都不敏感。
func phash(gray []uint8) uint64 {
	field := make([][]float64, sampleSide)
	flat := make([]float64, sampleSide*sampleSide)
	for y := 0; y < sampleSide; y++ {
		field[y] = flat[y*sampleSide : (y+1)*sampleSide]
		for x := 0; x < sampleSide; x++ {
			field[y][x] = float64(gray[y*sampleSide+x])
		}
	}
	for y := 0; y < sampleSide; y++ {
		dct1d(field[y])
	}
	col := make([]float64, sampleSide)
	for x := 0; x < sampleSide; x++ {
		for y := 0; y < sampleSide; y++ {
			col[y] = field[y][x]
		}
		dct1d(col)
		for y := 0; y < sampleSide; y++ {
			field[y][x] = col[y]
		}
	}
	// 左上 8×8 是低频，直流项 (0,0) 只反映平均亮度，丢掉。
	values := make([]float64, 0, lowFreq*lowFreq-1)
	for y := 0; y < lowFreq; y++ {
		for x := 0; x < lowFreq; x++ {
			if x == 0 && y == 0 {
				continue
			}
			values = append(values, field[y][x])
		}
	}
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	var h uint64
	for i, v := range values {
		if v > median {
			h |= 1 << uint(i)
		}
	}
	return h
}

// Hamming 两个指纹差几位，0 到 63。
func Hamming(a, b uint64) int {
	x := a ^ b
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

// Similar 两张图算不算重复。
//
// 两张都是平坦图（纯色、纯白底、纯黑底）时，除非 opts.IgnoreFlatColor，
// 否则还要求平均色接近：平坦图的低频系数本来就相同，不加这一条，
// 纯白底主图和纯黑底主图会互相判重复。
func Similar(a, b Signature, opts Options) bool {
	if a.Pixels != "" && a.Pixels == b.Pixels {
		return true
	}
	if opts.MaxDistance <= 0 {
		return false
	}
	if Hamming(a.Hash, b.Hash) > opts.MaxDistance {
		return false
	}
	if !opts.IgnoreFlatColor && a.Flat && b.Flat && colorGap(a, b) > flatColorGap {
		return false
	}
	return true
}

// colorGap 是两张图平均色差的最大通道值。
func colorGap(a, b Signature) int {
	gap := func(x, y uint8) int {
		if x > y {
			return int(x) - int(y)
		}
		return int(y) - int(x)
	}
	return max(gap(a.MeanR, b.MeanR), gap(a.MeanG, b.MeanG), gap(a.MeanB, b.MeanB))
}

// Group 一组重复的图。Keep 是留下的那张在输入里的下标，Dups 是同一组的其余下标。
type Group struct {
	Keep int
	Dups []int
	// Distance 是组内离 Keep 最远的汉明距离，报告里写它。
	Distance int
}

// Group 把指纹分组。用并查集，组与组之间连起来也算一组。
//
// 两两比较是 O(n²)，但一次只看异或后的位数：一万张约五千万次，几十毫秒。
// 真到几十万张再按哈希分桶，现在不值当。
func GroupInto(sigs []Signature, opts Options) []Group {
	n := len(sigs)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(i, j int) {
		ri, rj := find(i), find(j)
		if ri != rj {
			parent[rj] = ri
		}
	}
	exact := map[string]int{}
	for i := range sigs {
		if p := sigs[i].Pixels; p != "" {
			if j, ok := exact[p]; ok {
				union(i, j)
				continue
			}
			exact[p] = i
		}
		for j := 0; j < i; j++ {
			if find(j) == find(i) {
				continue
			}
			if Similar(sigs[j], sigs[i], opts) {
				union(i, j)
			}
		}
	}
	buckets := map[int][]int{}
	var order []int
	for i := range sigs {
		r := find(i)
		if _, ok := buckets[r]; !ok {
			order = append(order, r)
		}
		buckets[r] = append(buckets[r], i)
	}
	sort.SliceStable(order, func(x, y int) bool { return order[x] < order[y] })
	var groups []Group
	for _, root := range order {
		members := buckets[root]
		if len(members) < 2 {
			continue
		}
		keep := members[0]
		for _, m := range members[1:] {
			if better(sigs[m], sigs[keep]) {
				keep = m
			}
		}
		g := Group{Keep: keep}
		for _, m := range members {
			if m == keep {
				continue
			}
			g.Dups = append(g.Dups, m)
			if d := Hamming(sigs[keep].Hash, sigs[m].Hash); d > g.Distance {
				g.Distance = d
			}
		}
		groups = append(groups, g)
	}
	return groups
}

// better 挑保留哪张：先看像素数，再看文件大小，最后按路径排，保证同样的输入给同样的结果。
func better(a, b Signature) bool {
	if ap, bp := a.Pixels64(), b.Pixels64(); ap != bp {
		return ap > bp
	}
	if a.Bytes != b.Bytes {
		return a.Bytes > b.Bytes
	}
	return a.Path < b.Path
}

// Report 是一次去重的结果。Groups 里的下标对应 Run 拿到的 paths（按去重后的顺序）。
type Report struct {
	OutDir     string
	Options    Options
	Scanned    int
	Unique     int
	Duplicates int
	Groups     []Group
	Signatures []Signature
	BytesSaved int64
	Failed     []string
}

// Run 扫描 paths，把不重复的图复制到 outDir，再写一份 csv 报告。
// progress 可以为 nil；给了就一张一张地串行回调（done, total），不会并发进入。
func Run(paths []string, outDir string, opts Options, progress func(done, total int)) (Report, error) {
	rep := Report{OutDir: outDir, Options: opts}
	files := dedupePaths(paths)
	total := len(files)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return rep, fmt.Errorf("创建输出文件夹: %w", err)
	}
	sigs := make([]Signature, total)
	errs := make([]string, total)
	done := 0
	var mu sync.Mutex
	jobs := make(chan int)
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers > total {
		workers = total
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				sig, err := Hash(files[i])
				if err != nil {
					errs[i] = err.Error()
				} else {
					sigs[i] = sig
				}
				// 进度回调在锁里串行调用：调用方可以直接写自己的计数变量，不用再包一层。
				mu.Lock()
				done++
				if progress != nil {
					progress(done, total)
				}
				mu.Unlock()
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	// 解不开的文件剔掉，剩下的下标重新连续。
	kept := make([]Signature, 0, total)
	for i := range sigs {
		if errs[i] != "" {
			rep.Failed = append(rep.Failed, errs[i])
			continue
		}
		kept = append(kept, sigs[i])
	}
	rep.Signatures = kept
	rep.Scanned = len(kept)
	rep.Groups = GroupInto(kept, opts)
	dupOf := map[int]bool{}
	for _, g := range rep.Groups {
		for _, d := range g.Dups {
			dupOf[d] = true
			rep.BytesSaved += kept[d].Bytes
		}
	}
	rep.Duplicates = len(dupOf)
	rep.Unique = len(kept) - rep.Duplicates
	used := map[string]bool{}
	for i := range kept {
		if dupOf[i] {
			continue
		}
		if err := copyFile(kept[i].Path, outDir, used); err != nil {
			rep.Failed = append(rep.Failed, err.Error())
			rep.Unique--
		}
	}
	if err := writeReport(rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// dedupePaths 去掉重复路径，保持原顺序。
func dedupePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// copyFile 复制一张图，重名时加 _2、_3，不覆盖输出目录里已有的文件。
func copyFile(src, dir string, used map[string]bool) error {
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	name := base
	for n := 2; used[name] || exists(filepath.Join(dir, name)); n++ {
		name = fmt.Sprintf("%s_%d%s", stem, n, ext)
	}
	dst := filepath.Join(dir, name)
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", base, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("写入 %s: %w", name, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("写入 %s: %w", name, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("写入 %s: %w", name, err)
	}
	used[name] = true
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeReport 写一份 csv：每组一行，留下哪张、删掉哪些、差几位。
func writeReport(rep Report) error {
	path := filepath.Join(rep.OutDir, ReportName)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("写报告: %w", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"组", "保留", "重复", "差异位数"}); err != nil {
		return err
	}
	for i, g := range rep.Groups {
		var dups []string
		for _, d := range g.Dups {
			dups = append(dups, rep.Signatures[d].Path)
		}
		row := []string{
			fmt.Sprint(i + 1),
			rep.Signatures[g.Keep].Path,
			strings.Join(dups, " | "),
			fmt.Sprint(g.Distance),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}
