package cdp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCamoufoxAutomationSignals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; worker-src blob:")
		if r.URL.Path == "/probe.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`try { eval('1+2'); document.documentElement.dataset.csp='allowed' } catch (_) { document.documentElement.dataset.csp='blocked' }`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/frame" {
			_, _ = w.Write([]byte(`<!doctype html><body>frame</body>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><script src="/probe.js"></script><body><iframe src="/frame"></iframe></body>`))
	}))
	defer server.Close()
	s := newFixtureSession(t, server.URL)
	s.AppendNavigateScript(`void 0`)
	if err := s.Navigate(server.URL); err != nil {
		t.Fatal(err)
	}
	s.Handle("cspProbe", func(string) string { return `{"ok":true}` })
	answer, err := s.EvaluateAsync(`tulaiCall('cspProbe','')`)
	var reply struct{ OK bool }
	if err != nil || json.Unmarshal([]byte(answer), &reply) != nil || !reply.OK {
		t.Fatalf("CSP 下页面桥不可用: %s, %v", answer, err)
	}
	value, err := s.EvaluateAsync(`(async()=>{
 let reads=0;const error=new Error('probe');
 Object.defineProperty(error,'stack',{get(){reads++;return 'stack'}});
 console.log(error);await new Promise(r=>setTimeout(r,250));
 const frame=document.querySelector('iframe').contentWindow;
 const url=URL.createObjectURL(new Blob(['postMessage(navigator.webdriver ?? null)'],{type:'text/javascript'}));
 const worker=new Worker(url);
 const workerWebdriver=await new Promise((resolve,reject)=>{worker.onmessage=e=>resolve(e.data);worker.onerror=reject});
 worker.terminate();URL.revokeObjectURL(url);
 const names=Object.getOwnPropertyNames(document.defaultView).filter(k=>/^(\$?cdc_|__webdriver|__driver|__fxdriver|__selenium|__playwright|__pw|domAutomation|__tulai|tulai)/i.test(k));
 const frameNames=Object.getOwnPropertyNames(frame).filter(k=>/^(?:__tulai|tulai)/i.test(k));
 return {webdriver:navigator.webdriver??null,iframeWebdriver:frame.navigator.webdriver??null,workerWebdriver,stackReads:reads,names,frameNames,userAgent:navigator.userAgent,base:window.__tulaiBaseInstalled,literal:'__tulai stays unchanged',csp:document.documentElement.dataset.csp};
})()`)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Webdriver       *bool    `json:"webdriver"`
		IframeWebdriver *bool    `json:"iframeWebdriver"`
		WorkerWebdriver *bool    `json:"workerWebdriver"`
		StackReads      int      `json:"stackReads"`
		Names           []string `json:"names"`
		FrameNames      []string `json:"frameNames"`
		UserAgent       string   `json:"userAgent"`
		Base            int      `json:"base"`
		Literal         string   `json:"literal"`
		CSP             string   `json:"csp"`
	}
	if err := json.Unmarshal([]byte(value), &probe); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []*bool{probe.Webdriver, probe.IframeWebdriver, probe.WorkerWebdriver} {
		if flag != nil && *flag {
			t.Fatalf("webdriver exposed in a page, iframe or worker: %s", value)
		}
	}
	if probe.Base != 3 || probe.Literal != "__tulai stays unchanged" || probe.CSP != "blocked" || probe.StackReads != 0 || len(probe.Names) != 0 || len(probe.FrameNames) != 0 || strings.Contains(strings.ToLower(probe.UserAgent), "headless") {
		t.Fatalf("automation detection probe reported a signal: %s", value)
	}
	value, err = s.Evaluate(`(()=>{Object.defineProperty(document.defaultView,'__tulaiPageCanary',{value:'site',configurable:false,writable:false});window.__tulaiPageCanary='private';return [window.__tulaiPageCanary,document.defaultView.__tulaiPageCanary]})()`)
	if err != nil || value != `["private","site"]` {
		t.Fatalf("页面变量干扰了应用私有状态: %s, %v", value, err)
	}
}
