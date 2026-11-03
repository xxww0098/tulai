package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestXiaohongshuNoteID(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://www.xiaohongshu.com/explore/676a35670000000013002578?xsec_token=private", true},
		{"https://www.xiaohongshu.com/discovery/item/676a35670000000013002578", true},
		{"https://www.xiaohongshu.com/user/profile/5f2e12340000000001008400/676a35670000000013002578", true},
		{"https://xiaohongshu.com/explore/676A35670000000013002578", true},
		{"https://xiaohongshu.com.evil.test/explore/676a35670000000013002578", false},
		{"https://user@xiaohongshu.com/explore/676a35670000000013002578", false},
		{"http://xiaohongshu.com/explore/676a35670000000013002578", false},
		{"https://xiaohongshu.com:8443/explore/676a35670000000013002578", false},
		{"https://xiaohongshu.com/explore", false},
		{"https://xiaohongshu.com/explore/not-a-note", false},
		{"https://xiaohongshu.com/user/profile/676a35670000000013002578", false},
		{"https://xiaohongshu.com/user/profile/invalid/676a35670000000013002578", false},
	} {
		page, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		_, got := xiaohongshuNoteID(page)
		if got != tc.want {
			t.Errorf("xiaohongshuNoteID(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestXiaohongshuProfileAndNoteLinks(t *testing.T) {
	const profileID = "5f2e12340000000001008400"
	const noteID = "676a35670000000013002578"
	page, _ := url.Parse("https://www.xiaohongshu.com/user/profile/" + profileID + "?xsec_token=profile")
	if got, ok := xiaohongshuProfileID(page); !ok || got != profileID {
		t.Fatalf("profile ID = %q, %v", got, ok)
	}
	for _, raw := range []string{
		"https://user@www.xiaohongshu.com/user/profile/" + profileID,
		"https://www.xiaohongshu.com:444/user/profile/" + profileID,
		"http://www.xiaohongshu.com/user/profile/" + profileID,
		"https://www.xiaohongshu.com.evil.test/user/profile/" + profileID,
		"https://www.xiaohongshu.com/user/profile/" + profileID + "/" + noteID,
	} {
		u, _ := url.Parse(raw)
		if _, ok := xiaohongshuProfileID(u); ok {
			t.Errorf("unsafe profile matched: %s", raw)
		}
	}
	for _, tc := range []struct {
		name, id, href string
		want           bool
	}{
		{"normal", noteID, "/user/profile/" + profileID + "/" + noteID + "?xsec_token=synthetic&xsec_source=pc_user&tracking=secret", true},
		{"other profile", noteID, "/user/profile/ffffffffffffffffffffffff/" + noteID, false},
		{"other note", noteID, "/user/profile/" + profileID + "/ffffffffffffffffffffffff", false},
		{"outside host", noteID, "https://evil.test/user/profile/" + profileID + "/" + noteID, false},
		{"credentials", noteID, "https://user@www.xiaohongshu.com/user/profile/" + profileID + "/" + noteID, false},
		{"port", noteID, "https://www.xiaohongshu.com:444/user/profile/" + profileID + "/" + noteID, false},
		{"duplicate token", noteID, "/user/profile/" + profileID + "/" + noteID + "?xsec_token=a&xsec_token=b", false},
		{"malformed query", noteID, "/user/profile/" + profileID + "/" + noteID + "?xsec_token=%zz", false},
		{"oversize token", noteID, "/user/profile/" + profileID + "/" + noteID + "?xsec_token=" + strings.Repeat("x", 2049), false},
		{"other route", noteID, "/explore/" + noteID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := xiaohongshuProfileNote(page, profileID, xiaohongshuProfileLink{ID: tc.id, Href: tc.href})
			if ok != tc.want {
				t.Fatalf("link matched=%v, want %v", ok, tc.want)
			}
			if ok && (got.Query().Get("xsec_token") != "synthetic" || got.Query().Get("xsec_source") != "pc_user" || got.Query().Has("tracking")) {
				t.Fatalf("unexpected query: %s", got.RawQuery)
			}
		})
	}
}

func TestXiaohongshuImageChoicesAndIncompleteResults(t *testing.T) {
	page, _ := url.Parse("https://www.xiaohongshu.com/explore/676a35670000000013002578?xsec_token=private")
	first := "https://sns-webpic-qc.xhscdn.com/a?sign=keep"
	second := "https://ci.xiaohongshu.com/b"
	snapshot := xiaohongshuSnapshot{Status: "ok", Title: "相册", Total: 5, Images: []xiaohongshuImageURLs{
		{Primary: []string{first}, Fallback: []string{"https://sns-webpic-qc.xhscdn.com/preview"}},
		{Primary: []string{"https://xhscdn.com.evil.test/a"}, Fallback: []string{second}},
		{Primary: []string{first}},
		{Primary: []string{"https://bad:password@sns-webpic-qc.xhscdn.com/a", "http://127.0.0.1/private"}},
	}}
	got, err := xiaohongshuImages(page, snapshot)
	if !got.IsPage || got.Title != "相册" || len(got.Items) != 2 || got.Items[0].URL != first || got.Items[1].URL != second {
		t.Fatalf("note result = %+v, err = %v", got, err)
	}
	if err == nil || !strings.Contains(err.Error(), "没有可用地址") || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("missing/truncated result not reported: %v", err)
	}
	for _, item := range got.Items {
		if ref := item.Headers["Referer"]; ref != "https://www.xiaohongshu.com/" || strings.Contains(ref, "private") {
			t.Fatalf("cross-origin referer = %q", ref)
		}
	}
	if _, err := xiaohongshuImages(page, xiaohongshuSnapshot{Status: "ok"}); err == nil {
		t.Fatal("empty note did not report missing images")
	}
}

func TestXiaohongshuKeepsDistinctOriginalVariants(t *testing.T) {
	page, _ := url.Parse("https://www.xiaohongshu.com/explore/676a35670000000013002578")
	first := "https://sns-webpic-qc.xhscdn.com/first"
	second := "https://sns-webpic-qc.xhscdn.com/second"
	snapshot := xiaohongshuSnapshot{Status: "ok", Total: 1, Images: []xiaohongshuImageURLs{{
		Primary: []string{first, second, first}, Fallback: []string{"https://sns-webpic-qc.xhscdn.com/preview"},
	}}}
	got, err := xiaohongshuImages(page, snapshot)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != first || got.Items[1].URL != second {
		t.Fatalf("distinct variants = %+v, %v", got.Items, err)
	}
}

func TestXiaohongshuImageHost(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://sns-webpic-qc.xhscdn.com/file", true},
		{"https://ci.xiaohongshu.com/file", true},
		{"https://xhscdn.com.evil.test/file", false},
		{"https://user@sns-webpic-qc.xhscdn.com/file", false},
		{"https://sns-webpic-qc.xhscdn.com:8443/file", false},
		{"http://127.0.0.1/file", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := xiaohongshuImageHost(u); got != tc.want {
			t.Errorf("xiaohongshuImageHost(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestXiaohongshuRenderedState(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	const id = "676a35670000000013002578"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><script>window.__INITIAL_STATE__ = {note: {noteDetailMap: new Map([
          ["676a35670000000013002578", {note: {title:"示例", imageList:[
            {url:"https://sns-webpic-qc.xhscdn.com/direct",urlDefault:"https://sns-webpic-qc.xhscdn.com/preview",urlPre:"https://sns-webpic-qc.xhscdn.com/preview-small",infoList:[{imageScene:"WB_DFT",url:"https://sns-webpic-qc.xhscdn.com/one"},{imageScene:"WB_DFT",url:"https://sns-webpic-qc.xhscdn.com/one-alt"},{url:"https://sns-webpic-qc.xhscdn.com/no-scene"},{imageScene:"WB_PRV",url:"https://sns-webpic-qc.xhscdn.com/not-original"}]},
            {info_list:[{image_scene:"WB_PRV",url:"https://sns-webpic-qc.xhscdn.com/small"},{image_scene:"WB_DFT",url:"https://sns-webpic-qc.xhscdn.com/two"}]}
          ]}}],
          ["ffffffffffffffffffffffff", {note:{imageList:[{urlDefault:"https://sns-webpic-qc.xhscdn.com/other"}]}}]
        ])}};</script></html>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	location := server.URL + "/explore/" + id
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), location, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var got xiaohongshuSnapshot
	if err := session.run(ctx, chromedp.Evaluate(fmt.Sprintf(xiaohongshuNoteScript, location, id), &got)); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || got.Title != "示例" || got.Total != 2 || len(got.Images) != 2 || len(got.Images[0].Primary) == 0 || len(got.Images[1].Primary) == 0 || got.Images[0].Primary[0] != "https://sns-webpic-qc.xhscdn.com/one" || got.Images[1].Primary[0] != "https://sns-webpic-qc.xhscdn.com/two" {
		t.Fatalf("snapshot = %+v", got)
	}
	variants := map[string]bool{}
	for _, value := range got.Images[0].Primary {
		variants[value] = true
	}
	for _, value := range []string{"one", "one-alt", "no-scene", "direct"} {
		if !variants["https://sns-webpic-qc.xhscdn.com/"+value] {
			t.Fatalf("matching variant %q missing: %+v", value, got.Images[0])
		}
	}
	if len(variants) != 4 {
		t.Fatalf("preview or unrelated variant included: %+v", got.Images[0])
	}
	if err := session.run(ctx, chromedp.Evaluate(`window.__INITIAL_STATE__.note.noteDetailMap=new Map([["676a35670000000013002578",{note:{image_list:[{info_list:[{image_scene:"WB_DFT",url:"https://sns-webpic-qc.xhscdn.com/snake"},{image_scene:"WB_PRV",url:"https://sns-webpic-qc.xhscdn.com/preview"}]}]}}]])`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.Evaluate(fmt.Sprintf(xiaohongshuNoteScript, location, id), &got)); err != nil || got.Status != "ok" || len(got.Images) != 1 || len(got.Images[0].Primary) != 1 || got.Images[0].Primary[0] != "https://sns-webpic-qc.xhscdn.com/snake" {
		t.Fatalf("snake-case snapshot = %+v, %v", got, err)
	}
	if err := session.run(ctx, chromedp.Evaluate(fmt.Sprintf(xiaohongshuNoteScript, location+"?other", id), &got)); err != nil || got.Status != "changed" {
		t.Fatalf("navigation guard = %+v, %v", got, err)
	}
}

func TestXiaohongshuCaptureCurrentNoteOnly(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><title>placeholder</title></html>")
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	const target = "https://www.xiaohongshu.com/explore/676a35670000000013002578?xsec_token=synthetic"
	const targetProfile = "https://www.xiaohongshu.com/user/profile/5f2e12340000000001008400/676a35670000000013002578?xsec_token=synthetic"
	const note = `<html><head><title>页面标题</title><script>
window.__INITIAL_STATE__ = {note:{noteDetailMap:{
  "676a35670000000013002578":{note:{title:"笔记",imageList:[
    {urlDefault:"https://sns-webpic-qc.xhscdn.com/one"},
    {urlDefault:"https://sns-webpic-qc.xhscdn.com/two"}]}},
  "ffffffffffffffffffffffff":{note:{imageList:[{urlDefault:"https://sns-webpic-qc.xhscdn.com/other-note"}]}}
}}};</script></head><body><img src="https://unrelated.invalid/recommendation.jpg"></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && (paused.Request.URL == target || paused.Request.URL == targetProfile) {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(note)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case responses <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(target)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || !got.IsPage || got.Title != "笔记" || len(got.Items) != 2 ||
		got.Items[0].URL != "https://sns-webpic-qc.xhscdn.com/one" || got.Items[1].URL != "https://sns-webpic-qc.xhscdn.com/two" {
		t.Fatalf("captured note = %+v, err = %v", got, err)
	}
	if session.PageURL() != target {
		t.Fatalf("captured page URL = %q", session.PageURL())
	}
	for _, item := range got.Items {
		if ref := item.Headers["Referer"]; ref != "https://www.xiaohongshu.com/" {
			t.Fatalf("captured image referer = %q", ref)
		}
	}
	if err := session.run(ctx, camoufox.Navigate(targetProfile)); err != nil {
		t.Fatal(err)
	}
	profileNote, err := session.Capture(ctx)
	if err != nil || len(profileNote.Items) != 2 || profileNote.Items[0].URL != "https://sns-webpic-qc.xhscdn.com/one" || session.PageURL() != targetProfile {
		t.Fatalf("profile note = %+v, page URL = %q, err = %v", profileNote, session.PageURL(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestXiaohongshuCaptureProfileInSeparateTab(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	const profileID = "5f2e12340000000001008400"
	const first = "676a35670000000013002578"
	const second = "776a35670000000013002578"
	const wrongAuthor = "876a35670000000013002578"
	const redirect = "976a35670000000013002578"
	profilePath := "/user/profile/" + profileID
	var escapedRequests, acceptedCookies, strippedQueries atomic.Int32
	var delaySecond atomic.Bool
	secondStarted := make(chan struct{}, 1)
	secondCanceled := make(chan struct{}, 1)
	releaseSecond := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "www.xiaohongshu.com" {
			escapedRequests.Add(1)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == profilePath {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/", Secure: true})
			_, _ = fmt.Fprintf(w, `<html><head><title>示例主页</title><style>body {height: 6000px}</style></head><body>
<div id="userPageContainer"><div id="userPostedFeeds">
  <section class="note-item" data-note-id="%s"><a class="cover" href="%s/%s?xsec_token=one&amp;tracking=discard">one</a></section>
  <section class="note-item" data-note-id="%s"><a class="cover" href="%s/%s?xsec_token=one">duplicate</a></section>
  <section class="note-item" data-note-id="%s"><a class="cover" href="%s/%s">other author</a></section>
  <section class="note-item" data-note-id="%s"><a class="cover" href="%s/%s">redirect</a></section>
  <section class="note-item" data-note-id="%s"><a class="cover" href="https://evil.invalid/user/profile/%s/%s">outside</a></section>
</div></div>
<script>let added=false; addEventListener('scroll',()=>{if(added)return; added=true;
document.getElementById('userPostedFeeds').insertAdjacentHTML('beforeend', '<section class="note-item" data-note-id="%s"><a class="cover" href="%s/%s?xsec_token=two">two</a></section>');});</script>
</body></html>`, first, profilePath, first, first, profilePath, first,
				wrongAuthor, profilePath, wrongAuthor, redirect, profilePath, redirect,
				second, profileID, second, second, profilePath, second)
			return
		}
		noteID := strings.TrimPrefix(r.URL.Path, profilePath+"/")
		if noteID != first && noteID != second && noteID != wrongAuthor && noteID != redirect {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.Header.Get("Cookie"), "session=synthetic") {
			acceptedCookies.Add(1)
		}
		if r.URL.Query().Get("tracking") == "" {
			strippedQueries.Add(1)
		}
		if noteID == redirect {
			http.Redirect(w, r, "https://evil.invalid/steal", http.StatusFound)
			return
		}
		if noteID == second && delaySecond.Load() {
			secondStarted <- struct{}{}
			select {
			case <-r.Context().Done():
				secondCanceled <- struct{}{}
				return
			case <-releaseSecond:
			}
		}
		author := profileID
		if noteID == wrongAuthor {
			author = "ffffffffffffffffffffffff"
		}
		_, _ = fmt.Fprintf(w, `<html><script>window.__INITIAL_STATE__={note:{noteDetailMap:{"%s":{note:{title:"笔记 %s",user:{userId:"%s"},imageList:[{infoList:[{imageScene:"WB_DFT",url:"https://sns-webpic-qc.xhscdn.com/%s.jpg"}]}]}}}}};</script></html>`, noteID, noteID, author, noteID)
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://www.xiaohongshu.com" + profilePath)
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String()), chromedp.Evaluate(`window.scrollTo(0, 100)`, nil)); err != nil {
		t.Fatal(err)
	}
	var originalY float64
	if err := session.run(ctx, chromedp.Evaluate(`window.scrollY`, &originalY)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "不完整") || !strings.Contains(err.Error(), "作者") || len(got.Items) != 2 {
		t.Fatalf("profile images=%+v err=%v", got.Items, err)
	}
	if got.Items[0].URL != "https://sns-webpic-qc.xhscdn.com/"+first+".jpg" || got.Items[1].URL != "https://sns-webpic-qc.xhscdn.com/"+second+".jpg" {
		t.Fatalf("profile images = %+v", got.Items)
	}
	if got.Items[0].Metadata["album_id"] != first || got.Items[1].Metadata["album_id"] != second {
		t.Fatalf("profile note metadata = %+v", got.Items)
	}
	if escapedRequests.Load() != 0 || acceptedCookies.Load() != 4 || strippedQueries.Load() != 4 {
		t.Fatalf("requests outside=%d authenticated=%d stripped=%d", escapedRequests.Load(), acceptedCookies.Load(), strippedQueries.Load())
	}
	var current string
	var finalY float64
	if err := session.run(ctx, chromedp.Location(&current), chromedp.Evaluate(`window.scrollY`, &finalY)); err != nil {
		t.Fatal(err)
	}
	if current != page.String() || finalY != originalY {
		t.Fatalf("original page changed: url=%s scroll=%g, expected scroll=%g", current, finalY, originalY)
	}
	delaySecond.Store(true)
	cancelCtx, cancelCapture := context.WithCancel(ctx)
	type captureResult struct {
		discovery Discovery
		err       error
	}
	done := make(chan captureResult, 1)
	go func() {
		discovery, err := session.Capture(cancelCtx)
		done <- captureResult{discovery, err}
	}()
	select {
	case <-secondStarted:
	case <-time.After(15 * time.Second):
		cancelCapture()
		t.Fatal("second synthetic note was not requested")
	}
	cancelCapture()
	select {
	case canceled := <-done:
		if !errors.Is(canceled.err, context.Canceled) || len(canceled.discovery.Items) != 1 || canceled.discovery.Items[0].Metadata["album_id"] != first {
			t.Fatalf("canceled profile result = %+v, err=%v", canceled.discovery.Items, canceled.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("profile capture did not stop after cancellation")
	}
	select {
	case <-secondCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled profile request stayed connected")
	}
	go func() {
		discovery, err := session.Capture(ctx)
		done <- captureResult{discovery, err}
	}()
	select {
	case <-secondStarted:
	case <-time.After(15 * time.Second):
		t.Fatal("second synthetic note was not requested for navigation test")
	}
	if err := session.run(ctx, camoufox.Navigate(page.String()+"?tab=note")); err != nil {
		t.Fatal(err)
	}
	releaseSecond <- struct{}{}
	select {
	case navigated := <-done:
		if navigated.err == nil || !strings.Contains(navigated.err.Error(), "主页已更改") || len(navigated.discovery.Items) != 1 || navigated.discovery.Items[0].Metadata["album_id"] != first {
			t.Fatalf("changed page result = %+v, err=%v", navigated.discovery.Items, navigated.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("profile capture did not detect navigation")
	}
}
