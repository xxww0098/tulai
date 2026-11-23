package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/camoufoxtest"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestManagedChromeSubmitsTenPerMessageAndMapsReversedResults(t *testing.T) {
	config := os.Getenv("TULAI_CHROME_TEST_DATA")
	if _, ok := camoufox.InstalledChrome(config); !ok || testing.Short() {
		t.Skip("requires a tulai-managed Chrome test runtime")
	}
	testDirectWashBatches(t, config, store.EngineChromeCDP, 23)
}

func TestCamoufoxDriverSubmitsTenPerMessageAndMapsReversedResults(t *testing.T) {
	testDirectWashBatches(t, camoufoxtest.ConfigDir(t), "", 21)
}

func testDirectWashBatches(t *testing.T, config, engine string, count int) {
	t.Helper()
	const page = `<!doctype html><meta charset="utf-8"><div class="guidance-input-surface">
		<button data-testid="skill_input_exit_button">图像生成</button>
		<textarea data-testid="chat_input_input"></textarea>
		<button data-testid="upload_file_button" onclick="picker.click()">上传图片</button>
		<input hidden type="file" id="picker"><div id="cards"></div>
		<button data-testid="chat_input_send_button" id="submit">发送</button></div>
		<script>
		const batch = [];
		picker.onchange = async () => {
			const file = picker.files[0], id = new Uint8Array(await file.arrayBuffer()).at(-1);
			batch.push({id, name: file.name});
			const card = document.createElement('div');
			card.dataset.testid = 'attachment-image-card';
			card.setAttribute('aria-label', file.name);
			card.textContent = file.name; cards.append(card);
		};
		submit.onclick = async () => {
			const box = document.querySelector('textarea'), prompt = box.value;
			const attachments = batch.map(({id, name}) => ({image:{name, uri:'src/'+id}}));
			const creations = batch.slice().reverse().map(({id}) => ({image:{
				image_ori_raw:{url:'https://p3.byteimg.com/result/'+id+'.png'}, gen_params:{img_uris:['src/'+id]}
			}}));
			const url = '/chat/' + batch[0].id;
			const state = {message_list:[
				{index:'1',content_block:[{content:{attachment_block:{attachments}}}]},
				{index:'2',content_block:[{content:{creation_block:{creations}}}]}
			]};
			await fetch('/submit', {method:'POST', body:JSON.stringify({url,state,ids:batch.map(f=>f.id),
				named:batch.length===1 || batch.every(f=>prompt.includes(f.name))})});
			history.pushState({}, '', url); box.value = ''; cards.replaceChildren();
			const user = document.createElement('div'); user.dataset.role = 'user';
			user.textContent = prompt; document.body.append(user);
			const reply = document.createElement('div'); reply.dataset.role = 'assistant'; reply.dataset.finished = 'true';
			for (const {image} of creations) {
				const card = document.createElement('div'); card.dataset.testid = 'mdbox_image';
				const img = new Image(256,256); img.src = image.image_ori_raw.url; card.append(img); reply.append(card);
			}
			document.body.append(reply);
		};
		</script>`
	sources := make([]string, count)
	ids := make([]int, count)
	outputs := make(map[string][]byte)
	for i := range sources {
		ids[i] = i + 1
		// All sources have the same basename. The final byte identifies the
		// actual uploaded file independently of the displayed attachment name.
		sources[i] = filepath.Join(t.TempDir(), "source.png")
		if err := os.WriteFile(sources[i], append(resultPNG(t, byte(i+1)), byte(i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		outputs[fmt.Sprintf("/result/%d.png", i+1)] = resultPNG(t, byte(60+i))
	}
	var mu sync.Mutex
	var batches [][]int
	states := map[string]json.RawMessage{}
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/submit":
			var sent struct {
				URL   string
				State json.RawMessage
				IDs   []int
				Named bool
			}
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil || !sent.Named {
				t.Errorf("invalid batch submission: named=%t, err=%v", sent.Named, err)
			}
			batches = append(batches, sent.IDs)
			states[sent.URL] = sent.State
		case outputs[r.URL.Path] != nil:
			w.Header().Set("Access-Control-Allow-Origin", "https://www.doubao.com")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(outputs[r.URL.Path])
		case states[r.URL.Path] != nil:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<script>window._ROUTER_DATA = %s;</script>", states[r.URL.Path])
		case r.URL.Path == "/chat/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, page)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fixture.Close()
	proxy := washFixtureProxy(fixture)
	defer proxy.Close()
	proc, err := camoufox.Start(t.Context(), config, camoufox.Options{
		ChromeCDP: engine == store.EngineChromeCDP, Profile: t.TempDir(), Headless: true, Proxy: proxy.URL, GeoIP: new(bool), IgnoreHTTPSErrors: true,
	}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	const handle = 62373
	camoufox.Register(handle, proc)
	defer camoufox.Unregister(handle, proc)
	endpoint := camoufox.Endpoint(handle)
	if engine == store.EngineChromeCDP {
		endpoint = camoufox.ChromeEndpoint(handle)
	}
	client, err := cdp.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	doubao := &strategy.Doubao{}
	sess, err := client.NewBackgroundSession(doubao.HomeURL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := doubao.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox")
	account, err := st.AddManagedBrowser("豆包", browser.CanonicalBackend(engine), "", engine)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	e.sessions[fmt.Sprintf("%s:豆包:%d", browser.CanonicalBackend(account.Engine), account.ID)] = sess
	opts := Options{Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: sources,
		Wash: true, WaitResult: true, WaitResultSeconds: 10, OutputPath: t.TempDir()}
	if err := e.RunObserved(t.Context(), opts, func(Task) {}, func(Progress) {}, nil); err != nil {
		t.Fatal(err)
	}
	items := e.View().Snapshot()
	if len(items) != count {
		t.Fatalf("tracked %d of %d images", len(items), count)
	}
	for i, item := range items {
		if item.Status != StatusSuccess || len(item.Results) != 1 {
			t.Fatalf("image %d: %s, %s", i+1, item.Status, item.Error)
		}
		got, err := os.ReadFile(item.Results[0])
		if err != nil || !bytes.Equal(got, outputs[fmt.Sprintf("/result/%d.png", i+1)]) {
			t.Fatalf("image %d saved another source's result: %s, err=%v", i+1, item.Results[0], err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	want := slices.Collect(slices.Chunk(ids, 10))
	if !slices.EqualFunc(batches, want, slices.Equal[[]int]) {
		t.Fatalf("sent %v", batches)
	}
}
