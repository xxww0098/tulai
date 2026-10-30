package dedup

import (
	"encoding/csv"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writePNG 画一张带渐变和色块的图，压成 png 存盘。
func writePNG(t *testing.T, path string, w, h int, seed uint8) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{
				R: uint8(x * 255 / max(1, w-1)),
				G: uint8(y * 255 / max(1, h-1)),
				B: seed,
				A: 255,
			}
			// 右上角一块深色，让 dHash 有位可翻。
			if x > w*2/3 && y < h/3 {
				c = color.RGBA{R: 12, G: 18, B: 24, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("编码 %s: %v", path, err)
	}
}

func TestHamming(t *testing.T) {
	if got := Hamming(0, 0); got != 0 {
		t.Fatalf("Hamming(0,0) = %d", got)
	}
	if got := Hamming(0b1011, 0b0010); got != 2 {
		t.Fatalf("Hamming(0b1011,0b0010) = %d, want 2", got)
	}
	if got := Hamming(^uint64(0), 0); got != 64 {
		t.Fatalf("Hamming(all,0) = %d", got)
	}
}

func TestHashIsStableAndPixelDigestSurvivesReEncode(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	writePNG(t, a, 64, 48, 200)
	writePNG(t, b, 64, 48, 200)

	sa, err := Hash(a)
	if err != nil {
		t.Fatalf("Hash(a): %v", err)
	}
	sa2, err := Hash(a)
	if err != nil {
		t.Fatalf("Hash(a) 第二次: %v", err)
	}
	if sa.Hash != sa2.Hash || sa.Pixels != sa2.Pixels {
		t.Fatalf("同一文件两次指纹不同: %+v vs %+v", sa, sa2)
	}
	sb, err := Hash(b)
	if err != nil {
		t.Fatalf("Hash(b): %v", err)
	}
	if sa.Pixels != sb.Pixels {
		t.Fatalf("像素相同的两张图摘要不同: %s vs %s", sa.Pixels, sb.Pixels)
	}
	if sa.Width != 64 || sa.Height != 48 {
		t.Fatalf("尺寸 = %dx%d", sa.Width, sa.Height)
	}
	if !Similar(sa, sb, Options{MaxDistance: 0}) {
		t.Fatal("像素相同的两张图在阈值 0 下应判为重复")
	}
}

func TestSimilarNearDuplicateByContent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	c := filepath.Join(dir, "c.png")
	writePNG(t, a, 200, 150, 30)
	writePNG(t, b, 200, 150, 30)
	writePNG(t, c, 200, 150, 30)
	// b 换一种内容：左下角补一块亮的，dHash 会差几位但不是全差。
	img, err := os.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(img)
	img.Close()
	if err != nil {
		t.Fatal(err)
	}
	patched := image.NewRGBA(decoded.Bounds())
	for y := 0; y < decoded.Bounds().Dy(); y++ {
		for x := 0; x < decoded.Bounds().Dx(); x++ {
			patched.Set(x, y, decoded.At(x, y))
		}
	}
	for y := 120; y < 150; y++ {
		for x := 0; x < 60; x++ {
			patched.Set(x, y, color.RGBA{R: 250, G: 250, B: 250, A: 255})
		}
	}
	out, err := os.Create(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(out, patched); err != nil {
		t.Fatal(err)
	}
	out.Close()
	// c 换整体亮度，dHash 应当不受影响。
	bright, err := png.Decode(mustOpen(t, c))
	if err != nil {
		t.Fatal(err)
	}
	shifted := image.NewRGBA(bright.Bounds())
	for y := 0; y < bright.Bounds().Dy(); y++ {
		for x := 0; x < bright.Bounds().Dx(); x++ {
			r, g, bb, _ := bright.At(x, y).RGBA()
			shifted.Set(x, y, color.RGBA{R: uint8(r >> 9), G: uint8(g >> 9), B: uint8(bb >> 9), A: 255})
		}
	}
	cw, err := os.Create(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(cw, shifted); err != nil {
		t.Fatal(err)
	}
	cw.Close()

	sa, err := Hash(a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := Hash(b)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := Hash(c)
	if err != nil {
		t.Fatal(err)
	}
	if d := Hamming(sa.Hash, sc.Hash); d > 4 {
		t.Fatalf("整体压暗后 dHash 差 %d 位，应基本不变", d)
	}
	if Hamming(sa.Hash, sb.Hash) == 0 {
		t.Fatal("加了色块的图 dHash 完全没变，采样有问题")
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestGroupIntoPicksLargestAndChains(t *testing.T) {
	sigs := []Signature{
		{Path: "/x/small.png", Width: 40, Height: 40, Bytes: 100, Pixels: "p1", Hash: 0x00},
		{Path: "/x/big.png", Width: 400, Height: 400, Bytes: 300, Pixels: "p1", Hash: 0x00},
		{Path: "/x/near.png", Width: 40, Height: 40, Bytes: 120, Pixels: "p2", Hash: 0x03},
		{Path: "/x/other.png", Width: 40, Height: 40, Bytes: 120, Pixels: "p3", Hash: ^uint64(0)},
	}
	groups := GroupInto(sigs, Options{MaxDistance: 2})
	if len(groups) != 1 {
		t.Fatalf("组数 = %d, want 1: %+v", len(groups), groups)
	}
	g := groups[0]
	if sigs[g.Keep].Path != "/x/big.png" {
		t.Fatalf("保留的是 %s，应留分辨率最大的", sigs[g.Keep].Path)
	}
	if len(g.Dups) != 2 {
		t.Fatalf("重复数 = %d, want 2", len(g.Dups))
	}
	// 阈值 2 时 other（差 64 位）不该被并进来。
	if len(GroupInto(sigs, Options{MaxDistance: 0})) != 1 {
		t.Fatal("阈值 0 时也应保留像素完全相同的那一组")
	}
}

func TestRunCopiesUniqueAndWritesReport(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(src, "a.png")
	b := filepath.Join(src, "b.png")
	c := filepath.Join(src, "c.png")
	writePNG(t, a, 200, 200, 10)
	writePNG(t, b, 200, 200, 10) // 和 a 像素完全一致
	writePNG(t, c, 80, 60, 90)   // 另一张
	broken := filepath.Join(src, "broken.png")
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	var lastDone, lastTotal int
	rep, err := Run([]string{a, b, c, broken, a}, out, Options{MaxDistance: 0}, func(done, total int) {
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if lastTotal != 4 || lastDone != 4 {
		t.Fatalf("进度 = %d/%d, want 4/4", lastDone, lastTotal)
	}
	if rep.Scanned != 3 {
		t.Fatalf("Scanned = %d, want 3", rep.Scanned)
	}
	if len(rep.Failed) != 1 {
		t.Fatalf("Failed = %v, want 1 条解码失败", rep.Failed)
	}
	if len(rep.Groups) != 1 || rep.Duplicates != 1 {
		t.Fatalf("Groups = %+v, Duplicates = %d", rep.Groups, rep.Duplicates)
	}
	if rep.Unique != 2 {
		t.Fatalf("Unique = %d, want 2", rep.Unique)
	}
	for _, name := range []string{"a.png", "c.png", ReportName} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("输出缺少 %s: %v", name, err)
		}
	}
	f, err := os.Open(filepath.Join(out, ReportName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("报告行数 = %d, want 表头 + 1 组: %v", len(rows), rows)
	}
}

func TestCopyFileNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "keep.png")
	writePNG(t, src, 16, 16, 1)
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// 输出目录里已经有同名文件，复制后要改名，原文件内容不动。
	existing := filepath.Join(out, "keep.png")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	if err := copyFile(src, out, used); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "old" {
		t.Fatalf("已有文件被覆盖: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "keep_2.png")); err != nil {
		t.Fatalf("没有改名成 keep_2.png: %v", err)
	}
}

// TestDifferentStructureIsNotGrouped 是回归测试：早先的 dHash 实现里，
// 「左边一道竖条」和「整幅竖渐变」都算不出任何位，两张不相干的图被判成同一组。
func TestDifferentStructureIsNotGrouped(t *testing.T) {
	dir := t.TempDir()
	band := filepath.Join(dir, "band.png")
	grad := filepath.Join(dir, "grad.png")
	bandImg := image.NewNRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			c := color.NRGBA{R: 250, G: 236, B: 214, A: 255}
			if x < 320/3 {
				c = color.NRGBA{R: 226, G: 74, B: 74, A: 255}
			}
			bandImg.Set(x, y, c)
		}
	}
	gradImg := image.NewNRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			gradImg.Set(x, y, color.NRGBA{R: 40, G: uint8(120 + 100*y/240), B: 210, A: 255})
		}
	}
	for name, img := range map[string]image.Image{band: bandImg, grad: gradImg} {
		f, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	sb, err := Hash(band)
	if err != nil {
		t.Fatal(err)
	}
	sg, err := Hash(grad)
	if err != nil {
		t.Fatal(err)
	}
	if sb.Hash == sg.Hash {
		t.Fatalf("两张结构不同的图指纹相同: %016x", sb.Hash)
	}
	if got := len(GroupInto([]Signature{sb, sg}, Options{MaxDistance: dedupTestThreshold})); got != 0 {
		t.Fatalf("不该分出一组，得到 %d 组", got)
	}
}

// dedupTestThreshold 是回归测试用的阈值，和界面默认值一致。
const dedupTestThreshold = 8

// TestFlatColorGuardCanBeTurnedOff 盯住纯色底这一条：默认挡住纯白底和纯黑底，
// 关掉护栏后两张平坦图才会并到一组。
func TestFlatColorGuardCanBeTurnedOff(t *testing.T) {
	white := Signature{Path: "/a/white.png", Width: 500, Height: 500, Bytes: 1, Pixels: "w", Hash: 0x00, MeanR: 255, MeanG: 255, MeanB: 255, Flat: true}
	black := Signature{Path: "/a/black.png", Width: 500, Height: 500, Bytes: 1, Pixels: "b", Hash: 0x00, MeanR: 3, MeanG: 3, MeanB: 4, Flat: true}
	if Similar(white, black, Options{MaxDistance: 8}) {
		t.Fatal("纯白底和纯黑底默认不该判重复")
	}
	if got := len(GroupInto([]Signature{white, black}, Options{MaxDistance: 8})); got != 0 {
		t.Fatalf("默认分组 = %d 组, want 0", got)
	}
	if !Similar(white, black, Options{MaxDistance: 8, IgnoreFlatColor: true}) {
		t.Fatal("关掉护栏后应判重复")
	}
	if got := len(GroupInto([]Signature{white, black}, Options{MaxDistance: 8, IgnoreFlatColor: true})); got != 1 {
		t.Fatalf("关掉护栏后分组 = %d 组, want 1", got)
	}
}

// TestFlatGuardKeepsBrightnessShiftedCopy 是护栏不能误伤的场景：
// 同一张照片整体压暗后两张都不算平坦，照样要判成重复。
func TestFlatGuardKeepsBrightnessShiftedCopy(t *testing.T) {
	a := Signature{Path: "/a/orig.png", Width: 400, Height: 300, Pixels: "x", Hash: 0x1234, MeanR: 180, MeanG: 160, MeanB: 140}
	b := Signature{Path: "/a/dark.png", Width: 400, Height: 300, Pixels: "y", Hash: 0x1234, MeanR: 90, MeanG: 80, MeanB: 70}
	if !Similar(a, b, Options{MaxDistance: 8}) {
		t.Fatal("有纹理的图压暗后应判重复")
	}
}
