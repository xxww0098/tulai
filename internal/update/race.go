package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	probeBytes         = 512 << 10
	rangeChunkSize     = 2 << 20
	rangeConcurrency   = 6
	maxBufferedChunks  = 12
	probeConnectTimeout = 8 * time.Second
	probeWindow        = 1500 * time.Millisecond
	chunkStallTimeout  = 12 * time.Second
	slowCheckWindow    = 6 * time.Second
	minSpeedBPS        = 256 << 10
	fastRangeWinBPS    = 3 << 20
)

type speedSample struct {
	at   time.Time
	done int64
}

type speedMeter struct {
	mu      sync.Mutex
	samples []speedSample
	maxDone int64
	ema     float64
}

func newSpeedMeter() *speedMeter {
	return &speedMeter{samples: make([]speedSample, 0, 32)}
}

func (m *speedMeter) record(done int64) float64 {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if done < m.maxDone {
		done = m.maxDone
	} else {
		m.maxDone = done
	}
	m.samples = append(m.samples, speedSample{at: now, done: done})
	cutoff := now.Add(-2500 * time.Millisecond)
	trim := 0
	for trim < len(m.samples)-1 && m.samples[trim].at.Before(cutoff) {
		trim++
	}
	if trim > 0 {
		m.samples = append(m.samples[:0], m.samples[trim:]...)
	}
	if len(m.samples) < 2 {
		return m.ema
	}
	oldest := m.samples[0]
	dt := now.Sub(oldest.at).Seconds()
	if dt < 0.15 || done <= oldest.done {
		return m.ema
	}
	raw := float64(done-oldest.done) / dt
	if m.ema <= 0 {
		m.ema = raw
	} else {
		m.ema = m.ema*0.55 + raw*0.45
	}
	return m.ema
}

type contentRangeInfo struct {
	start int64
	end   int64
	total int64
}

func parseContentRange(header string) (contentRangeInfo, bool) {
	header = strings.TrimSpace(header)
	if len(header) < 6 || !strings.EqualFold(header[:6], "bytes ") {
		return contentRangeInfo{}, false
	}
	spec := strings.TrimSpace(header[6:])
	rangePart, totalPart, ok := strings.Cut(spec, "/")
	if !ok {
		return contentRangeInfo{}, false
	}
	startStr, endStr, ok := strings.Cut(rangePart, "-")
	if !ok {
		return contentRangeInfo{}, false
	}
	start, err1 := strconv.ParseInt(strings.TrimSpace(startStr), 10, 64)
	end, err2 := strconv.ParseInt(strings.TrimSpace(endStr), 10, 64)
	if err1 != nil || err2 != nil || start < 0 || end < start {
		return contentRangeInfo{}, false
	}
	var total int64
	if strings.TrimSpace(totalPart) != "*" {
		t, err3 := strconv.ParseInt(strings.TrimSpace(totalPart), 10, 64)
		if err3 != nil || t <= end {
			return contentRangeInfo{}, false
		}
		total = t
	}
	return contentRangeInfo{start: start, end: end, total: total}, true
}

type probeState struct {
	cand          downloadCandidate
	idx           int
	cancel        context.CancelFunc
	doneCh        chan struct{}
	resp          *http.Response
	buf           []byte
	totalSize     int64
	supportsRange bool
	finished      bool
	eof           bool
	err           error
	startedAt     time.Time
	firstByteAt   time.Time
	doneAt        time.Time
	settled       bool
}

func (p *probeState) speed() float64 {
	if len(p.buf) == 0 {
		return 0
	}
	end := p.doneAt
	if end.IsZero() {
		end = time.Now()
	}
	sec := end.Sub(p.startedAt).Seconds()
	if sec < 0.01 {
		sec = 0.01
	}
	return float64(len(p.buf)) / sec
}

func (p *probeState) closeBody() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.resp != nil && p.resp.Body != nil {
		_ = p.resp.Body.Close()
		p.resp = nil
	}
}

func (c *Client) downloadLargeAsset(ctx context.Context, cands []downloadCandidate, asset Asset, dest string, reportProgress bool) ([]string, string, int64, error) {
	winner, rankedRange, err := c.probeDownloadSources(ctx, cands, asset)
	if err != nil {
		return nil, "", 0, err
	}
	usedSet := map[string]bool{winner.cand.url: true}
	collectUsed := func(extra []string) []string {
		for _, u := range extra {
			usedSet[u] = true
		}
		out := make([]string, 0, len(usedSet))
		for u := range usedSet {
			out = append(out, u)
		}
		return out
	}

	tmp := dest + ".partial"
	os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		winner.closeBody()
		return collectUsed(nil), winner.cand.label, 0, err
	}

	report := func(done, total int64, bps float64, source string, force bool) {}
	if reportProgress {
		report = c.makeProgressReporter(asset.Name)
	}

	if winner.supportsRange {
		initialSpeed := winner.speed()
		written, writeErr := f.Write(winner.buf)
		total := winner.totalSize
		if writeErr != nil {
			_ = f.Close()
			winner.closeBody()
			return collectUsed(nil), winner.cand.label, total, writeErr
		}
		winner.buf = nil
		winner.closeBody()
		lastLabel := winner.cand.label
		report(int64(written), total, initialSpeed, lastLabel, int64(written) >= total)
		if int64(written) < total {
			ordered := buildOrderedRangeSources(winner.cand, rankedRange, cands)
			used, rangeLabel, rerr := c.downloadRanges(ctx, f, ordered, int64(written), total, asset.Name, report)
			closeErr := f.Close()
			if rangeLabel != "" {
				lastLabel = rangeLabel
			}
			if rerr != nil {
				return collectUsed(used), lastLabel, total, rerr
			}
			if closeErr != nil {
				return collectUsed(used), lastLabel, total, closeErr
			}
			for _, u := range used {
				usedSet[u] = true
			}
		} else if closeErr := f.Close(); closeErr != nil {
			return collectUsed(nil), lastLabel, total, closeErr
		}
		if err := finalizeTempDownload(tmp, dest, total, maxDownload); err != nil {
			return collectUsed(nil), lastLabel, total, err
		}
		report(total, total, 0, lastLabel, true)
		return collectUsed(nil), lastLabel, total, nil
	}

	// Non-Range 200 OK stream with stall + low-speed failover to Range mirrors.
	fallbacks := make([]downloadCandidate, 0, len(cands))
	for _, rc := range rankedRange {
		if rc.url != winner.cand.url {
			fallbacks = append(fallbacks, rc)
		}
	}
	for _, cand := range cands {
		if cand.url == winner.cand.url {
			continue
		}
		already := false
		for _, fb := range fallbacks {
			if fb.url == cand.url {
				already = true
				break
			}
		}
		if !already {
			fallbacks = append(fallbacks, cand)
		}
	}

	downloaded, completed, totalSize, streamErr := c.streamFullResponse(ctx, f, winner, fallbacks, asset, report)
	winner.closeBody()
	if !completed {
		if totalSize > 0 && downloaded > 0 && downloaded < totalSize && len(fallbacks) > 0 && ctx.Err() == nil {
			used, rangeLabel, rerr := c.downloadRanges(ctx, f, fallbacks, downloaded, totalSize, asset.Name, report)
			closeErr := f.Close()
			if rangeLabel == "" {
				rangeLabel = fallbacks[0].label
			}
			if rerr == nil && closeErr == nil {
				if err := finalizeTempDownload(tmp, dest, totalSize, maxDownload); err != nil {
					return collectUsed(used), rangeLabel, totalSize, err
				}
				report(totalSize, totalSize, 0, rangeLabel, true)
				return collectUsed(used), rangeLabel, totalSize, nil
			}
			return collectUsed(used), rangeLabel, totalSize, firstNonNil(rerr, closeErr)
		}
		_ = f.Close()
		if streamErr == nil {
			streamErr = fmt.Errorf("下载 %s 中断", asset.Name)
		}
		return collectUsed(nil), winner.cand.label, totalSize, streamErr
	}
	if closeErr := f.Close(); closeErr != nil {
		return collectUsed(nil), winner.cand.label, totalSize, closeErr
	}
	finalTotal := totalSize
	if finalTotal <= 0 {
		finalTotal = downloaded
	}
	if err := finalizeTempDownload(tmp, dest, finalTotal, maxDownload); err != nil {
		return collectUsed(nil), winner.cand.label, finalTotal, err
	}
	report(finalTotal, finalTotal, 0, winner.cand.label, true)
	return collectUsed(nil), winner.cand.label, finalTotal, nil
}

func buildOrderedRangeSources(winner downloadCandidate, ranked []downloadCandidate, all []downloadCandidate) []downloadCandidate {
	out := make([]downloadCandidate, 0, len(all))
	seen := make(map[string]bool, len(all))
	add := func(c downloadCandidate) {
		if !seen[c.url] {
			seen[c.url] = true
			out = append(out, c)
		}
	}
	add(winner)
	for _, c := range ranked {
		add(c)
	}
	for _, c := range all {
		add(c)
	}
	return out
}

func finalizeTempDownload(tmp, dest string, wantSize, limit int64) error {
	info, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	got := info.Size()
	if got > limit {
		os.Remove(tmp)
		return fmt.Errorf("超过大小限制")
	}
	if wantSize > 0 && got != wantSize {
		return &shortError{got: got, want: wantSize}
	}
	os.Remove(dest)
	return os.Rename(tmp, dest)
}

func (c *Client) makeProgressReporter(name string) func(done, total int64, bps float64, source string, force bool) {
	var mu sync.Mutex
	var maxReported int64
	var lastSource string
	var lastEmit time.Time
	return func(done, total int64, bps float64, source string, force bool) {
		if c.OnProgress == nil {
			return
		}
		clamped := done
		if total > 0 && !force && clamped >= total {
			clamped = total - 1
		}
		if clamped < 0 {
			clamped = 0
		}
		now := time.Now()
		mu.Lock()
		sourceChanged := source != "" && source != lastSource
		if !force && !sourceChanged && clamped <= maxReported && !lastEmit.IsZero() && now.Sub(lastEmit) < 150*time.Millisecond {
			mu.Unlock()
			return
		}
		if !force && !sourceChanged && !lastEmit.IsZero() && now.Sub(lastEmit) < 100*time.Millisecond {
			mu.Unlock()
			return
		}
		if clamped > maxReported {
			maxReported = clamped
		}
		if source != "" {
			lastSource = source
		}
		reported := maxReported
		lastEmit = now
		mu.Unlock()
		c.OnProgress(Progress{
			Phase:       PhaseDownloading,
			Name:        name,
			Done:        reported,
			Total:       total,
			BytesPerSec: bps,
			Source:      source,
		})
	}
}

func (c *Client) probeDownloadSources(ctx context.Context, cands []downloadCandidate, asset Asset) (*probeState, []downloadCandidate, error) {
	probes := make([]*probeState, len(cands))
	for i, cand := range cands {
		probes[i] = &probeState{
			cand:      cand,
			idx:       i,
			doneCh:    make(chan struct{}),
			startedAt: time.Now(),
		}
	}

	var mu sync.Mutex
	settledCount := 0
	decided := false
	decisionCh := make(chan struct{})
	var windowTimer *time.Timer
	var firstDataAt time.Time

	closeDecision := func() {
		if decided {
			return
		}
		decided = true
		if windowTimer != nil {
			windowTimer.Stop()
		}
		close(decisionCh)
	}

	maybeDecideLocked := func() {
		if decided {
			return
		}
		if settledCount == len(probes) {
			closeDecision()
			return
		}
		for _, p := range probes {
			if p.finished && p.supportsRange && p.err == nil && p.speed() >= fastRangeWinBPS {
				// Honor higher-priority candidates (explicit mirror / proxy direct) if still probing.
				higherPending := false
				if c.PreferMirror || c.hasProxy() {
					for j := 0; j < p.idx; j++ {
						if !probes[j].settled && probes[j].err == nil {
							higherPending = true
							break
						}
					}
				}
				if !higherPending {
					closeDecision()
					return
				}
			}
		}
	}

	armWindowLocked := func() {
		if windowTimer != nil || decided || len(probes) <= 1 {
			return
		}
		windowTimer = time.AfterFunc(probeWindow, func() {
			mu.Lock()
			closeDecision()
			mu.Unlock()
		})
	}

	var wg sync.WaitGroup
	for _, p := range probes {
		wg.Add(1)
		go func(p *probeState) {
			defer wg.Done()
			defer close(p.doneCh)
			pCtx, pCancel := context.WithCancel(ctx)
			mu.Lock()
			p.cancel = pCancel
			mu.Unlock()

			c.runSingleProbe(pCtx, p, asset, &mu, &decided, &firstDataAt, armWindowLocked)

			mu.Lock()
			p.settled = true
			settledCount++
			maybeDecideLocked()
			mu.Unlock()
		}(p)
	}

	select {
	case <-decisionCh:
	case <-ctx.Done():
		mu.Lock()
		closeDecision()
		mu.Unlock()
	}

	mu.Lock()
	var usable []*probeState
	for _, p := range probes {
		if p.err == nil && (len(p.buf) > 0 || p.eof) {
			usable = append(usable, p)
		}
	}
	if len(usable) == 0 {
		mu.Unlock()
		wg.Wait()
		var errs []string
		for _, p := range probes {
			p.closeBody()
			if p.err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", p.cand.label, p.err))
			}
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if len(errs) == 0 {
			return nil, nil, errors.New("无可用下载源")
		}
		return nil, nil, errors.New(strings.Join(errs, "; "))
	}

	var max200Speed float64
	for _, p := range usable {
		if !p.supportsRange {
			if spd := p.speed(); spd > max200Speed {
				max200Speed = spd
			}
		}
	}

	scoreProbe := func(p *probeState) float64 {
		spd := p.speed()
		score := spd
		if p.supportsRange && (spd >= fastRangeWinBPS/2 || spd >= max200Speed*0.5) {
			score = spd * 2.5
		}
		if (c.PreferMirror && p.idx == 0) || (c.hasProxy() && p.cand.primary) {
			score *= 1.15
		}
		if p.eof && p.finished {
			score += 1e12
		}
		return score
	}

	sort.SliceStable(usable, func(i, j int) bool {
		return scoreProbe(usable[i]) > scoreProbe(usable[j])
	})
	best := usable[0]

	var rankedRange []downloadCandidate
	for _, p := range usable {
		if p.supportsRange {
			rankedRange = append(rankedRange, p.cand)
		}
	}
	mu.Unlock()

	// Cancel and wait for all non-selected probes before returning best so no background
	// probe goroutine reads or writes shared probe fields afterwards.
	for _, p := range probes {
		if p != best {
			mu.Lock()
			cancel := p.cancel
			mu.Unlock()
			if cancel != nil {
				cancel()
			}
			<-p.doneCh
			p.closeBody()
		}
	}
	if best.supportsRange {
		<-best.doneCh
		best.closeBody()
	} else {
		<-best.doneCh
	}

	return best, rankedRange, nil
}

func (c *Client) runSingleProbe(
	ctx context.Context,
	p *probeState,
	asset Asset,
	mu *sync.Mutex,
	decided *bool,
	firstDataAt *time.Time,
	armWindowLocked func(),
) {
	reqCtx, cancelReq := context.WithCancel(ctx)
	var gotFirstByte atomic.Bool
	connectTimer := time.AfterFunc(probeConnectTimeout, func() {
		if !gotFirstByte.Load() {
			mu.Lock()
			if p.err == nil {
				p.err = fmt.Errorf("连接超时")
			}
			mu.Unlock()
			cancelReq()
		}
	})
	defer connectTimer.Stop()
	defer func() {
		if p.err != nil || p.supportsRange {
			cancelReq()
		}
	}()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, p.cand.url, nil)
	if err != nil {
		mu.Lock()
		p.err = err
		mu.Unlock()
		return
	}
	end := int64(probeBytes - 1)
	if asset.Size > 0 && asset.Size < probeBytes {
		end = asset.Size - 1
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", end))
	c.decorate(req, "application/octet-stream")

	resp, err := c.http().Do(req)
	if err != nil {
		mu.Lock()
		if p.err == nil {
			p.err = err
		}
		mu.Unlock()
		return
	}

	var supportsRange bool
	var totalSize int64
	if resp.StatusCode == http.StatusPartialContent {
		cr, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || cr.start != 0 || cr.total <= 0 || cr.total > maxDownload {
			_ = resp.Body.Close()
			mu.Lock()
			p.err = errors.New("无效 Content-Range")
			mu.Unlock()
			return
		}
		if asset.Size > 0 && cr.total != asset.Size {
			_ = resp.Body.Close()
			mu.Lock()
			p.err = fmt.Errorf("文件大小不匹配（%d / %d）", cr.total, asset.Size)
			mu.Unlock()
			return
		}
		supportsRange = true
		totalSize = cr.total
	} else if resp.StatusCode == http.StatusOK {
		if resp.ContentLength > maxDownload {
			_ = resp.Body.Close()
			mu.Lock()
			p.err = fmt.Errorf("%s 超过大小限制", asset.Name)
			mu.Unlock()
			return
		}
		if asset.Size > 0 && resp.ContentLength > 0 && resp.ContentLength != asset.Size {
			_ = resp.Body.Close()
			mu.Lock()
			p.err = fmt.Errorf("文件大小不匹配（%d / %d）", resp.ContentLength, asset.Size)
			mu.Unlock()
			return
		}
		supportsRange = false
		if resp.ContentLength > 0 {
			totalSize = resp.ContentLength
		} else {
			totalSize = asset.Size
		}
	} else {
		_ = resp.Body.Close()
		mu.Lock()
		p.err = &statusError{
			code:    resp.StatusCode,
			msg:     fmt.Sprintf("HTTP %d", resp.StatusCode),
			tryNext: true,
		}
		mu.Unlock()
		return
	}

	mu.Lock()
	p.supportsRange = supportsRange
	p.totalSize = totalSize
	p.resp = resp
	mu.Unlock()

	expectedEnd := int64(probeBytes)
	if totalSize > 0 && totalSize < expectedEnd {
		expectedEnd = totalSize
	}

	// Guard against a stalled Read once decided is true or mid-probe.
	var lastProbeRead atomic.Int64
	lastProbeRead.Store(time.Now().UnixNano())
	probeWatchStop := make(chan struct{})
	defer close(probeWatchStop)
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-probeWatchStop:
				return
			case <-reqCtx.Done():
				return
			case now := <-ticker.C:
				last := time.Unix(0, lastProbeRead.Load())
				if now.Sub(last) >= chunkStallTimeout {
					mu.Lock()
					if p.err == nil {
						p.err = errors.New("测速读取超时")
					}
					mu.Unlock()
					cancelReq()
					return
				}
			}
		}
	}()

	tmpBuf := make([]byte, 32*1024)
	for {
		mu.Lock()
		stop := *decided
		mu.Unlock()
		if stop {
			break
		}
		n, rerr := resp.Body.Read(tmpBuf)
		if n > 0 {
			now := time.Now()
			lastProbeRead.Store(now.UnixNano())
			mu.Lock()
			if !gotFirstByte.Swap(true) {
				connectTimer.Stop()
				p.firstByteAt = now
				if firstDataAt.IsZero() {
					*firstDataAt = now
					armWindowLocked()
				}
			}
			p.buf = append(p.buf, tmpBuf[:n]...)
			curLen := int64(len(p.buf))
			if curLen >= expectedEnd {
				p.finished = true
				p.doneAt = now
				if totalSize > 0 && curLen >= totalSize {
					p.eof = true
				}
				mu.Unlock()
				break
			}
			mu.Unlock()
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				mu.Lock()
				p.eof = true
				p.finished = true
				p.doneAt = time.Now()
				if !gotFirstByte.Swap(true) {
					connectTimer.Stop()
				}
				mu.Unlock()
				break
			}
			mu.Lock()
			if !*decided && p.err == nil {
				p.err = rerr
			}
			mu.Unlock()
			break
		}
	}
	if supportsRange {
		_ = resp.Body.Close()
		mu.Lock()
		p.resp = nil
		mu.Unlock()
	}
}

func (c *Client) streamFullResponse(
	ctx context.Context,
	file *os.File,
	winner *probeState,
	fallbacks []downloadCandidate,
	asset Asset,
	report func(done, total int64, bps float64, source string, force bool),
) (int64, bool, int64, error) {
	var downloaded int64
	if len(winner.buf) > 0 {
		n, err := file.Write(winner.buf)
		downloaded += int64(n)
		winner.buf = nil
		if err != nil {
			return downloaded, false, winner.totalSize, err
		}
	}
	totalSize := winner.totalSize
	report(downloaded, totalSize, winner.speed(), winner.cand.label, winner.eof)
	if winner.eof || (totalSize > 0 && downloaded >= totalSize) {
		return downloaded, true, totalSize, nil
	}
	if winner.resp == nil || winner.resp.Body == nil {
		return downloaded, false, totalSize, errors.New("响应流已关闭")
	}

	body := winner.resp.Body
	meter := newSpeedMeter()
	meter.record(downloaded)

	var lastByteNano atomic.Int64
	lastByteNano.Store(time.Now().UnixNano())
	var downloadedAtomic atomic.Int64
	downloadedAtomic.Store(downloaded)
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	var watchdogErr atomic.Value
	doneWatch := make(chan struct{})
	defer close(doneWatch)

	if len(fallbacks) > 0 || totalSize > 0 {
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			windowStart := time.Now()
			windowBytes := downloadedAtomic.Load()
			for {
				select {
				case <-doneWatch:
					return
				case <-streamCtx.Done():
					return
				case now := <-ticker.C:
					last := time.Unix(0, lastByteNano.Load())
					if now.Sub(last) >= chunkStallTimeout {
						watchdogErr.Store(fmt.Errorf("%s 下载长时间无响应", asset.Name))
						_ = body.Close()
						cancelStream()
						return
					}
					if len(fallbacks) > 0 && totalSize > 0 && now.Sub(windowStart) >= slowCheckWindow {
						cur := downloadedAtomic.Load()
						dt := now.Sub(windowStart).Seconds()
						bps := float64(cur-windowBytes) / dt
						windowStart = now
						windowBytes = cur
						if bps < minSpeedBPS && c.hasRangeFallback(streamCtx, fallbacks, cur) {
							watchdogErr.Store(errors.New("下载源过慢，切换分段加速源"))
							_ = body.Close()
							cancelStream()
							return
						}
					}
				}
			}
		}()
	}

	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if downloaded+int64(n) > maxDownload {
				return downloaded, false, totalSize, fmt.Errorf("%s 超过大小限制", asset.Name)
			}
			wn, werr := file.Write(buf[:n])
			downloaded += int64(wn)
			downloadedAtomic.Store(downloaded)
			lastByteNano.Store(time.Now().UnixNano())
			if werr != nil {
				return downloaded, false, totalSize, werr
			}
			bps := meter.record(downloaded)
			report(downloaded, totalSize, bps, winner.cand.label, false)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if totalSize > 0 && downloaded < totalSize {
					return downloaded, false, totalSize, &shortError{got: downloaded, want: totalSize}
				}
				return downloaded, true, totalSize, nil
			}
			if stored, ok := watchdogErr.Load().(error); ok && ctx.Err() == nil {
				return downloaded, false, totalSize, stored
			}
			return downloaded, false, totalSize, err
		}
	}
}

func (c *Client) hasRangeFallback(ctx context.Context, candidates []downloadCandidate, offset int64) bool {
	for _, cand := range candidates {
		probeCtx, cancel := context.WithTimeout(ctx, probeConnectTimeout)
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, cand.url, nil)
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset))
		c.decorate(req, "application/octet-stream")
		resp, err := c.http().Do(req)
		if err == nil {
			ok := false
			if resp.StatusCode == http.StatusPartialContent {
				cr, parsed := parseContentRange(resp.Header.Get("Content-Range"))
				ok = parsed && cr.start == offset
			}
			_ = resp.Body.Close()
			cancel()
			if ok {
				return true
			}
			continue
		}
		cancel()
	}
	return false
}

type rangeChunk struct {
	index    int
	start    int64
	end      int64
	attempts int
}

func (c *Client) downloadRanges(
	ctx context.Context,
	file *os.File,
	candidates []downloadCandidate,
	startOffset, totalSize int64,
	assetName string,
	report func(done, total int64, bps float64, source string, force bool),
) ([]string, string, error) {
	var chunks []*rangeChunk
	for idx, start := 0, startOffset; start < totalSize; idx, start = idx+1, start+rangeChunkSize {
		end := start + rangeChunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		chunks = append(chunks, &rangeChunk{index: idx, start: start, end: end})
	}
	if len(chunks) == 0 {
		label := ""
		if len(candidates) > 0 {
			label = candidates[0].label
		}
		return nil, label, nil
	}

	var (
		mu             sync.Mutex
		cond           = sync.NewCond(&mu)
		activeIdx      = 0
		nonRange       = make(map[string]bool)
		sourceFails    = make(map[string]int)
		usedURLs       = make(map[string]bool)
		nextToAssign   = 0
		nextToWrite    = 0
		inFlightCount  = 0
		retryQueue     []*rangeChunk
		completed      = make(map[int][]byte)
		inFlightBytes  = make(map[int]int64)
		flushedBytes   = startOffset
		fatalErr       error
		windowStart    = time.Now()
		windowBytes    int64
		currentLabel   = candidates[0].label
	)

	meter := newSpeedMeter()
	meter.record(startOffset)

	progressBytesLocked := func() int64 {
		sum := flushedBytes
		for _, buf := range completed {
			sum += int64(len(buf))
		}
		for _, n := range inFlightBytes {
			sum += n
		}
		return sum
	}

	pickSourceLocked := func() (downloadCandidate, bool) {
		for offset := 0; offset < len(candidates); offset++ {
			idx := (activeIdx + offset) % len(candidates)
			cand := candidates[idx]
			if !nonRange[cand.url] {
				activeIdx = idx
				currentLabel = cand.label
				return cand, true
			}
		}
		return downloadCandidate{}, false
	}

	rotateLocked := func(failedURL string, markNonRange bool) {
		if markNonRange {
			nonRange[failedURL] = true
		}
		if candidates[activeIdx].url == failedURL {
			for step := 1; step < len(candidates); step++ {
				nextIdx := (activeIdx + step) % len(candidates)
				if !nonRange[candidates[nextIdx].url] {
					activeIdx = nextIdx
					currentLabel = candidates[nextIdx].label
					return
				}
			}
		}
	}

	hasAlternateLocked := func(curURL string) bool {
		for _, cand := range candidates {
			if cand.url != curURL && !nonRange[cand.url] {
				return true
			}
		}
		return false
	}

	writeSignal := make(chan struct{}, 1)
	writerDone := make(chan struct{})
	stopCh := make(chan struct{})

	// Wake condition waiters when context is canceled.
	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			if fatalErr == nil {
				fatalErr = ctx.Err()
			}
			cond.Broadcast()
			mu.Unlock()
		case <-stopCh:
		}
	}()

	// Dedicated ordered writer drains completed chunks without holding mu during I/O.
	go func() {
		defer close(writerDone)
		for {
			mu.Lock()
			var buf []byte
			var hasNext bool
			if fatalErr == nil {
				if b, ok := completed[nextToWrite]; ok {
					buf = b
					delete(completed, nextToWrite)
					hasNext = true
				}
			}
			finished := fatalErr != nil || (nextToWrite == len(chunks) && !hasNext)
			mu.Unlock()

			if hasNext {
				if _, err := file.Write(buf); err != nil {
					mu.Lock()
					if fatalErr == nil {
						fatalErr = err
					}
					cond.Broadcast()
					mu.Unlock()
					return
				}
				mu.Lock()
				flushedBytes += int64(len(buf))
				nextToWrite++
				cond.Broadcast()
				mu.Unlock()
				continue
			}
			if finished {
				return
			}
			<-writeSignal
		}
	}()

	nextChunk := func() (*rangeChunk, bool) {
		mu.Lock()
		defer mu.Unlock()
		for {
			if fatalErr != nil {
				return nil, false
			}
			if len(retryQueue) > 0 && retryQueue[0].index-nextToWrite < maxBufferedChunks {
				ch := retryQueue[0]
				retryQueue = retryQueue[1:]
				inFlightCount++
				return ch, true
			}
			if nextToAssign < len(chunks) && chunks[nextToAssign].index-nextToWrite < maxBufferedChunks {
				ch := chunks[nextToAssign]
				nextToAssign++
				inFlightCount++
				return ch, true
			}
			if nextToAssign >= len(chunks) && len(retryQueue) == 0 && inFlightCount == 0 {
				return nil, false
			}
			cond.Wait()
		}
	}

	maxAttempts := len(candidates) * 3
	if maxAttempts < 4 {
		maxAttempts = 4
	}
	workerCount := rangeConcurrency
	if len(chunks) < workerCount {
		workerCount = len(chunks)
	}

	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				chunk, ok := nextChunk()
				if !ok {
					return
				}
				mu.Lock()
				src, hasSrc := pickSourceLocked()
				if hasSrc {
					usedURLs[src.url] = true
				}
				mu.Unlock()
				if !hasSrc {
					mu.Lock()
					if fatalErr == nil {
						fatalErr = errors.New("所有下载源均不支持分段加速")
					}
					inFlightCount--
					cond.Broadcast()
					mu.Unlock()
					return
				}

				data, nonRangeErr, err := c.fetchRangeChunk(ctx, src.url, chunk, func(chunkGot, delta int64) {
					mu.Lock()
					inFlightBytes[chunk.index] = chunkGot
					windowBytes += delta
					totalDone := progressBytesLocked()
					now := time.Now()
					if now.Sub(windowStart) >= slowCheckWindow {
						dt := now.Sub(windowStart).Seconds()
						bps := float64(windowBytes) / dt
						windowStart = now
						windowBytes = 0
						if bps < minSpeedBPS && hasAlternateLocked(src.url) {
							rotateLocked(src.url, false)
						}
					}
					lbl := currentLabel
					mu.Unlock()
					bps := meter.record(totalDone)
					report(totalDone, totalSize, bps, lbl, false)
				})

				mu.Lock()
				delete(inFlightBytes, chunk.index)
				inFlightCount--
				if err == nil {
					sourceFails[src.url] = 0
					completed[chunk.index] = data
					totalDone := progressBytesLocked()
					lbl := currentLabel
					cond.Broadcast()
					mu.Unlock()
					select {
					case writeSignal <- struct{}{}:
					default:
					}
					bps := meter.record(totalDone)
					report(totalDone, totalSize, bps, lbl, false)
					continue
				}

				chunk.attempts++
				fails := sourceFails[src.url] + 1
				sourceFails[src.url] = fails
				if nonRangeErr || fails >= 2 {
					rotateLocked(src.url, nonRangeErr)
				}
				if chunk.attempts >= maxAttempts || (errors.Is(err, context.Canceled) && ctx.Err() != nil) {
					if fatalErr == nil {
						fatalErr = err
					}
					cond.Broadcast()
					mu.Unlock()
					return
				}
				pos := len(retryQueue)
				for i, queued := range retryQueue {
					if chunk.index < queued.index {
						pos = i
						break
					}
				}
				retryQueue = append(retryQueue, nil)
				copy(retryQueue[pos+1:], retryQueue[pos:])
				retryQueue[pos] = chunk
				cond.Broadcast()
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	select {
	case writeSignal <- struct{}{}:
	default:
	}
	<-writerDone
	close(stopCh)

	outUsed := make([]string, 0, len(usedURLs))
	for u := range usedURLs {
		outUsed = append(outUsed, u)
	}
	if fatalErr != nil {
		return outUsed, currentLabel, fatalErr
	}
	if nextToWrite != len(chunks) {
		return outUsed, currentLabel, fmt.Errorf("%s 下载未完整写入", assetName)
	}
	return outUsed, currentLabel, nil
}

func (c *Client) fetchRangeChunk(
	ctx context.Context,
	rawURL string,
	chunk *rangeChunk,
	onProgress func(chunkGot, delta int64),
) (data []byte, nonRange bool, err error) {
	chunkCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var lastByteNano atomic.Int64
	lastByteNano.Store(time.Now().UnixNano())
	var stalled atomic.Bool

	doneCh := make(chan struct{})
	defer close(doneCh)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-doneCh:
				return
			case <-chunkCtx.Done():
				return
			case now := <-ticker.C:
				last := time.Unix(0, lastByteNano.Load())
				if now.Sub(last) >= chunkStallTimeout {
					stalled.Store(true)
					cancel()
					return
				}
			}
		}
	}()

	req, err := http.NewRequestWithContext(chunkCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.start, chunk.end))
	c.decorate(req, "application/octet-stream")

	resp, err := c.http().Do(req)
	if err != nil {
		if stalled.Load() && ctx.Err() == nil {
			return nil, false, errors.New("分段下载超时")
		}
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil, true, fmt.Errorf("下载源不支持分段续传 (%s)", rawURL)
	}
	if resp.StatusCode != http.StatusPartialContent {
		return nil, false, &statusError{
			code:    resp.StatusCode,
			msg:     fmt.Sprintf("HTTP %d", resp.StatusCode),
			tryNext: true,
		}
	}
	cr, ok := parseContentRange(resp.Header.Get("Content-Range"))
	if !ok || cr.start != chunk.start || cr.end < chunk.end {
		return nil, false, fmt.Errorf("分段范围不匹配 (%s)", rawURL)
	}

	expected := chunk.end - chunk.start + 1
	var buf bytes.Buffer
	buf.Grow(int(expected))
	tmp := make([]byte, 32*1024)
	var got int64

	for got < expected {
		n, rerr := resp.Body.Read(tmp)
		if n > 0 {
			lastByteNano.Store(time.Now().UnixNano())
			remain := expected - got
			if int64(n) > remain {
				n = int(remain)
			}
			buf.Write(tmp[:n])
			got += int64(n)
			onProgress(got, int64(n))
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			if stalled.Load() && ctx.Err() == nil {
				return nil, false, errors.New("分段下载超时")
			}
			return nil, false, rerr
		}
	}
	if got < expected {
		return nil, false, fmt.Errorf("分段下载不完整 (%d/%d)", got, expected)
	}
	return buf.Bytes(), false, nil
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
