package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"xbs/internal/captcha"
)

type egoFixture struct {
	ctx            context.Context
	states         map[string][]egoState
	clicks         []string
	fills          []string
	downloads      []string
	data           []byte
	afterRead      func()
	clickErrors    map[string]error
	downloadErrors []error
	onDownload     func(int)
	presses        []string
	pressError     error
	uploadOpen     []string
	uploadLabel    string
	uploadFiles    []string
	blocked        bool
	autoSolve      bool
	manual         bool
}

func (f *egoFixture) Ctx() context.Context { return f.ctx }
func (f *egoFixture) EgoEvaluate(expression string, out any) error {
	if strings.Contains(expression, "xbs captcha:manual") {
		f.manual = true
		return nil
	}
	if strings.Contains(expression, "xbs captcha:verification") {
		data, _ := json.Marshal(map[string]any{"blocked": f.blocked, "autoSolve": f.autoSolve && !f.manual})
		return json.Unmarshal(data, out)
	}
	action := strings.TrimPrefix(strings.SplitN(expression, " */", 2)[0], "/* xbs ego:")
	rows := f.states[action]
	if len(rows) == 0 {
		if action != "preview" {
			return errors.New("unexpected state: " + action)
		}
		rows = []egoState{{}}
	}
	state := rows[0]
	if len(rows) > 1 {
		f.states[action] = rows[1:]
	}
	data, _ := json.Marshal(state)
	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	if f.afterRead != nil {
		f.afterRead()
	}
	return nil
}
func (f *egoFixture) EgoClick(selector string) error {
	f.clicks = append(f.clicks, selector)
	return f.clickErrors[selector]
}
func (f *egoFixture) EgoFill(selector, value string) error {
	f.fills = append(f.fills, selector+"="+value)
	return nil
}
func (f *egoFixture) EgoPress(key string) error {
	f.presses = append(f.presses, key)
	return f.pressError
}
func (f *egoFixture) EgoUpload(open []string, label string, files []string) error {
	f.uploadOpen, f.uploadLabel, f.uploadFiles = open, label, files
	return nil
}
func (f *egoFixture) EgoDownload(selector string, timeout time.Duration) ([]byte, error) {
	f.downloads = append(f.downloads, selector)
	if f.onDownload != nil {
		f.onDownload(len(f.downloads))
	}
	if len(f.downloadErrors) >= len(f.downloads) {
		if err := f.downloadErrors[len(f.downloads)-1]; err != nil {
			return nil, err
		}
	}
	return f.data, nil
}

func TestEgoSendUsesOfficialActionsOnce(t *testing.T) {
	f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{"composer": {
		{Composer: "#editor"},
		{Composer: "#editor", Value: "line1\nline2", Send: "#send"},
		{Composer: "#new-editor", Value: ""},
	}, "bindPrompt": {{}}}}
	if err := egoSend(f, "line1\nline2"); err != nil {
		t.Fatal(err)
	}
	if len(f.fills) != 1 || f.fills[0] != "#editor=line1\nline2" || len(f.clicks) != 1 || f.clicks[0] != "#send" {
		t.Fatalf("actions fill=%v click=%v", f.fills, f.clicks)
	}
}

func TestEgoVerificationStopsStateAndOfficialActions(t *testing.T) {
	for _, stage := range []string{"init", "mark", "upload", "clean", "composer", "results", "preview"} {
		t.Run(stage, func(t *testing.T) {
			f := &egoFixture{ctx: context.Background(), blocked: true}
			var err error
			switch stage {
			case "upload":
				err = egoUpload(f, []string{"source.png"})
			case "clean":
				err = egoClean(f)
			case "composer":
				err = egoSend(f, "prompt")
			case "results":
				_, err = egoWaitForResult(f, time.Second)
			case "preview":
				err = egoClosePreview(f, time.Now().Add(time.Second))
			default:
				err = egoEvaluate(f, stage, nil, nil)
			}
			if !errors.Is(err, captcha.ErrVerificationRequired) || !f.manual || len(f.clicks)+len(f.fills)+len(f.uploadFiles)+len(f.downloads)+len(f.presses) != 0 {
				t.Fatalf("error=%v manual=%v actions=%+v", err, f.manual, f)
			}
		})
	}
	f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{"composer": {{Composer: "#editor"}}}}
	f.afterRead = func() { f.blocked = true }
	if err := egoSend(f, "prompt"); !errors.Is(err, captcha.ErrVerificationRequired) || len(f.fills) != 0 {
		t.Fatalf("challenge appearing after state read must prevent fill: %v fills=%v", err, f.fills)
	}
}

func TestEgoVerificationPreservesPartialAndCancellationPriority(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		f := &egoFixture{ctx: ctx, data: []byte("completed download"), states: map[string][]egoState{"results": {{Downloads: []egoControl{{Selector: "#first", Key: "first"}, {Selector: "#second", Key: "second"}}}}}}
		f.onDownload = func(int) {
			f.blocked = true
			if canceled {
				cancel()
			}
		}
		results, err := egoWaitForResult(f, time.Second)
		cancel()
		if canceled {
			if !errors.Is(err, context.Canceled) || errors.Is(err, captcha.ErrVerificationRequired) || len(results) != 0 {
				t.Fatalf("cancellation must win: %v results=%v", err, results)
			}
		} else if !errors.Is(err, captcha.ErrVerificationRequired) || len(results) != 1 || string(results[0].Data) != "completed download" {
			t.Fatalf("manual verification must preserve completed data: %v results=%v", err, results)
		}
		if len(f.downloads) != 1 {
			t.Fatalf("second download must not start: %v", f.downloads)
		}
	}
	f := &egoFixture{ctx: context.Background(), blocked: true, autoSolve: true, states: map[string][]egoState{"composer": {{Composer: "#editor"}, {Composer: "#editor", Value: "prompt", Send: "#send"}, {Composer: "#editor"}}, "bindPrompt": {{}}}}
	if err := egoSend(f, "prompt"); err != nil || f.manual || len(f.clicks) != 1 {
		t.Fatalf("enabled supported automatic solving must remain available: %v actions=%+v", err, f)
	}
}

func TestEgoVerificationDuringFinalStateReadCannotReportSuccess(t *testing.T) {
	for _, stage := range []string{"init", "upload", "send"} {
		t.Run(stage, func(t *testing.T) {
			f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{
				"init":        {{}},
				"uploadStart": {{Open: []string{"#upload"}}},
				"upload":      {{Ready: true}},
				"composer":    {{Composer: "#editor"}, {Composer: "#editor", Value: "prompt", Send: "#send"}, {Composer: "#editor"}},
				"bindPrompt":  {{}},
			}}
			reads, finalRead := 0, map[string]int{"init": 1, "upload": 2, "send": 4}[stage]
			f.afterRead = func() {
				reads++
				f.blocked = reads == finalRead
			}
			var err error
			switch stage {
			case "upload":
				err = egoUpload(f, []string{"source.png"})
			case "send":
				err = egoSend(f, "prompt")
			default:
				err = egoEvaluate(f, "init", nil, nil)
			}
			if !errors.Is(err, captcha.ErrVerificationRequired) || !f.manual || reads != finalRead {
				t.Fatalf("stage completed despite new challenge: error=%v manual=%v reads=%d", err, f.manual, reads)
			}
			if stage == "send" && len(f.clicks) != 1 {
				t.Fatalf("accepted send must not be repeated: %v", f.clicks)
			}
		})
	}
}

func TestEgoUploadAlreadyOpenMenuUsesSDKMenuLabel(t *testing.T) {
	f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{
		"uploadStart": {{MenuLabel: "上传图片"}},
		"upload":      {{Ready: true}},
	}}
	if err := egoUpload(f, []string{"source.png"}); err != nil {
		t.Fatal(err)
	}
	if len(f.uploadOpen) != 0 || f.uploadLabel != "上传图片" || strings.Join(f.uploadFiles, ",") != "source.png" {
		t.Fatalf("upload open=%v label=%q files=%v", f.uploadOpen, f.uploadLabel, f.uploadFiles)
	}
}

func TestEgoSendRejectsDuplicateEditorText(t *testing.T) {
	f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{"composer": {
		{Composer: "#editor"},
		{Composer: "#editor", Value: "promptprompt", Send: "#send"},
	}}}
	if err := egoSend(f, "prompt"); err == nil || len(f.clicks) != 0 {
		t.Fatalf("err=%v clicks=%v", err, f.clicks)
	}
}

func TestEgoResultUsesDownloadAndWaitsForCurrentGeneration(t *testing.T) {
	f := &egoFixture{ctx: context.Background(), data: []byte("downloaded file"), states: map[string][]egoState{"results": {
		{Busy: true, Downloads: []egoControl{{Selector: "#download", Key: "result"}}},
		{Downloads: []egoControl{{Selector: "#download", Key: "result"}}},
	}}}
	results, err := egoWaitForResult(f, time.Second)
	if err != nil || len(results) != 1 || string(results[0].Data) != "downloaded file" || results[0].Src != "download" || results[0].URL != "" {
		t.Fatalf("results=%v err=%v", results, err)
	}
	if len(f.downloads) != 1 {
		t.Fatalf("download actions=%v", f.downloads)
	}
}

func TestEgoResultDownloadsEachNewAssistantPreview(t *testing.T) {
	images := []egoControl{{Selector: "#new-image-1", Key: "image1"}, {Selector: "#new-image-2", Key: "image2"}}
	f := &egoFixture{ctx: context.Background(), data: []byte{}, states: map[string][]egoState{"results": {
		{Images: images, Downloads: []egoControl{{Selector: "#one-inline-button", Key: "unassociated"}}},
		{Downloads: []egoControl{{Selector: "#preview-download", Key: "download"}}, Close: "#close"},
		{Images: images, Downloads: []egoControl{{Selector: "#preview-download", Key: "download"}}, Close: "#close"},
	}}}
	results, err := egoWaitForResult(f, 2*time.Second)
	if err != nil || len(results) != 2 || results[0].Data == nil || results[1].Data == nil {
		t.Fatalf("results=%v err=%v", results, err)
	}
	if strings.Join(f.clicks, ",") != "#new-image-1,#new-image-2" || len(f.downloads) != 2 || strings.Join(f.presses, ",") != "Escape,Escape" {
		t.Fatalf("clicks=%v downloads=%v presses=%v", f.clicks, f.downloads, f.presses)
	}
}

func TestEgoResultPreservesCompletedDownloadsOnLaterError(t *testing.T) {
	for _, stage := range []string{"second-download", "close-preview", "open-second-preview", "preview-remains-open", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			images := []egoControl{{Selector: "#image1", Key: "image1"}, {Selector: "#image2", Key: "image2"}}
			f := &egoFixture{ctx: ctx, data: []byte("complete first download"), states: map[string][]egoState{"results": {
				{Images: images},
				{Images: images, Downloads: []egoControl{{Selector: "#download", Key: "download"}}, Close: "#close"},
				{Images: images, Downloads: []egoControl{{Selector: "#download", Key: "download"}}, Close: "#close"},
			}}, clickErrors: make(map[string]error)}
			expected := errors.New("fixture failed at " + stage)
			switch stage {
			case "second-download":
				f.downloadErrors = []error{nil, expected}
			case "close-preview":
				f.pressError = expected
			case "open-second-preview":
				f.clickErrors["#image2"] = expected
			case "preview-remains-open":
				f.states["preview"] = []egoState{{}, {Preview: true}}
			case "cancel":
				f.onDownload = func(count int) {
					if count == 2 {
						cancel()
					}
				}
			}
			results, err := egoWaitForResult(f, 2*time.Second)
			if err == nil {
				t.Fatal("expected later failure")
			}
			if stage == "cancel" {
				if !errors.Is(err, context.Canceled) || len(results) != 0 {
					t.Fatalf("cancellation must not save partial data: results=%v err=%v", results, err)
				}
			} else if len(results) != 1 || string(results[0].Data) != "complete first download" {
				t.Fatalf("completed data lost: results=%v err=%v", results, err)
			}
		})
	}
}

func TestEgoResultRetryClosesLeftoverPreview(t *testing.T) {
	images := []egoControl{{Selector: "#image1", Key: "image1"}, {Selector: "#image2", Key: "image2"}}
	withDownload := egoState{Images: images, Downloads: []egoControl{{Selector: "#download", Key: "download"}}}
	failure := errors.New("temporary download failure")
	f := &egoFixture{
		ctx: context.Background(), data: []byte("completed download"),
		downloadErrors: []error{failure},
		states: map[string][]egoState{
			"preview": {{}, {Preview: true}, {}},
			"results": {{Images: images}, withDownload, {Images: images}, withDownload, withDownload},
		},
	}
	if results, err := egoWaitForResult(f, 2*time.Second); !errors.Is(err, failure) || len(results) != 0 || len(f.presses) != 0 {
		t.Fatalf("initial download failure must preserve its state: results=%v err=%v presses=%v", results, err, f.presses)
	}
	results, err := egoWaitForResult(f, 2*time.Second)
	if err != nil || len(results) != 2 || strings.Join(f.clicks, ",") != "#image1,#image1,#image2" || strings.Join(f.presses, ",") != "Escape,Escape,Escape" {
		t.Fatalf("retry must close the leftover preview before reopening task images: results=%v err=%v clicks=%v presses=%v", results, err, f.clicks, f.presses)
	}
}

func TestEgoCleanClosesOnlyRecognizedImagePreview(t *testing.T) {
	f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{
		"clean":   {{Preview: true}, {}},
		"preview": {{Preview: true}, {}},
	}}
	if err := egoClean(f); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.presses, ",") != "Escape" || len(f.clicks) != 0 {
		t.Fatalf("actions=%v presses=%v", f.clicks, f.presses)
	}
	f = &egoFixture{ctx: context.Background(), states: map[string][]egoState{"clean": {{}}}}
	if err := egoClean(f); err != nil || len(f.presses) != 0 {
		t.Fatalf("ordinary dialog must remain untouched: err=%v presses=%v", err, f.presses)
	}
}

func TestEgoClosePreviewUsesKnownCloseOnlyAfterEscapeFails(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		states := []egoState{{Close: "#preview-close"}}
		if fallback {
			states = []egoState{{Preview: true, Close: "#preview-close"}, {Preview: true, Close: "#preview-close"}, {}}
		}
		f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{"preview": states}}
		if err := egoClosePreview(f, time.Now().Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
		wantClicks := 0
		if fallback {
			wantClicks = 1
		}
		if strings.Join(f.presses, ",") != "Escape" || len(f.clicks) != wantClicks || fallback && f.clicks[0] != "#preview-close" {
			t.Fatalf("fallback=%v presses=%v clicks=%v", fallback, f.presses, f.clicks)
		}
	}
}

func TestEgoCancelAfterStateReadDoesNotDownload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &egoFixture{ctx: ctx, states: map[string][]egoState{"results": {
		{Downloads: []egoControl{{Selector: "#download", Key: "result"}}},
	}}}
	reads := 0
	f.afterRead = func() {
		reads++
		if reads == 2 {
			cancel()
		}
	}
	_, err := egoWaitForResult(f, time.Second)
	if !errors.Is(err, context.Canceled) || len(f.downloads) != 0 || reads != 2 {
		t.Fatalf("err=%v downloads=%v reads=%d", err, f.downloads, reads)
	}
}

func TestEgoReadOnlyDOMFixtures(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable for isolated DOM fixtures")
	}
	script := `const assert=require('node:assert/strict');
const run=` + egoJS + `;
global.window={};global.Node={DOCUMENT_POSITION_FOLLOWING:4};global.location={hostname:'www.doubao.com'};global.CSS={escape:s=>s};global.getComputedStyle=el=>({display:el.hidden?'none':'block',visibility:'visible'});
function descendants(root){return root.childNodes.filter(x=>x.nodeType===1).flatMap(x=>[x,...descendants(x)])}
function matches(el,query){
 if(query==='*')return true;
 if(query==='p,div,span,[data-testid]')return /^(P|DIV|SPAN)$/.test(el.tagName)||el.hasAttribute('data-testid');
 if(query==='span,p,div')return /^(SPAN|P|DIV)$/.test(el.tagName);
 if(query==='#qk-markdown-react,.markdown-pc-special-class')return el.id==='qk-markdown-react'||String(el.getAttribute('class')||'').split(/\s+/).includes('markdown-pc-special-class');
 if(query==='[role="menu"] [role="menuitem"]'){if(el.getAttribute('role')!=='menuitem')return false;for(let p=el.parentElement;p;p=p.parentElement)if(p.getAttribute('role')==='menu')return true;return false}
 if(query==='[role="dialog"],dialog[open],[aria-modal="true"]')return el.getAttribute('role')==='dialog'||el.getAttribute('aria-modal')==='true';
 if(query.includes('data-message-author-role'))return query.includes('"'+el.attributes['data-role']+'"')||query.includes('.chat-question-card-wrap')&&String(el.getAttribute('class')||'').split(' ').includes('chat-question-card-wrap')||query.includes('.chat-answers-card-wrap')&&String(el.getAttribute('class')||'').split(' ').includes('chat-answers-card-wrap');
 if(query==='img')return el.tagName==='IMG';
 if(query==='button,[role="button"],a[download]')return el.tagName==='BUTTON'||el.getAttribute('role')==='button';
 if(query==='button[data-testid="upload_file_button"],button[aria-label="添加附件"]')return el.tagName==='BUTTON'&&(el.getAttribute('data-testid')==='upload_file_button'||el.getAttribute('aria-label')==='添加附件');
 if(query.startsWith('#'))return el.id===query.slice(1);
 const presence=query.match(/^\[([a-z-]+)\]$/);if(presence)return el.hasAttribute(presence[1]);
 const attr=query.match(/^([a-z]+)?\[([a-z-]+)(\^?=)("(?:[^"\\]|\\.)*")\]$/);
 return !!attr&&(!attr[1]||el.tagName.toLowerCase()===attr[1])&&(attr[3]==='^='?String(el.getAttribute(attr[2])||'').startsWith(JSON.parse(attr[4])):el.getAttribute(attr[2])===JSON.parse(attr[4]));
}
class Element {
 constructor(tag,id,attrs={}){this.tagName=tag;this.id=id;this.attributes=attrs;this.nodeType=1;this.childNodes=[];this.isConnected=true;this.innerText='';}
 getAttribute(name){return name==='id'?this.id:(this.attributes[name]||null)}
 getAttributeNames(){return ['id',...Object.keys(this.attributes)]}
 hasAttribute(name){return !!this.getAttribute(name)}
 getBoundingClientRect(){for(let node=this;node;node=node.parentElement)if(node.hidden)return {left:0,top:0,width:0,height:0};return {left:0,top:0,width:this.zeroRect?0:100,height:this.zeroRect?0:100}}
 closest(query){for(let node=this;node;node=node.parentElement)if((query==='.guidance-input-surface'&&node.getAttribute('class')==='guidance-input-surface')||matches(node,query))return node;return null}
 querySelector(query){return this.querySelectorAll(query)[0]||null}
 querySelectorAll(query){return descendants(this).filter(el=>matches(el,query))}
 matches(query){return matches(this,query)}
 compareDocumentPosition(node){const ordered=messages.flatMap(message=>[message,...descendants(message)]).concat(images);return ordered.indexOf(this)<ordered.lastIndexOf(node)?4:2}
 contains(node){return this===node||this.childNodes.some(child=>child===node||child.contains&&child.contains(node))}
}
const body=new Element('BODY','body'),html=new Element('HTML','html'),comp=new Element('DIV','editor',{'contenteditable':'true'});
comp.parentElement=body;body.parentElement=html;
body.childNodes=[comp];
let messages=[],images=[];
let hit=null;
global.document={body,documentElement:html,querySelectorAll(query){
 if(query.startsWith('textarea,'))return [comp];
 if(query==='img')return images;
 if(query==='[data-testid="mdbox_image"] img')return images.filter(img=>img.parentElement&&img.parentElement.closest('[data-testid="mdbox_image"]'));
 if(query.includes('data-message-author-role'))return messages.filter(el=>matches(el,query));
 return Array.from(new Set([body,...descendants(body),...messages.flatMap(message=>[message,...descendants(message)]),...images])).filter(el=>matches(el,query));
},elementFromPoint(){return hit}};
function text(value){return {nodeType:3,nodeValue:value}}
const placeholder=new Element('SPAN','placeholder',{'contenteditable':'false'});placeholder.childNodes=[text('向千问提问')];
comp.childNodes=[text('\uFEFF'),placeholder];assert.equal(run('composer').value,'');
comp.childNodes=[text('向千问提问')];assert.equal(run('composer').value,'向千问提问');
const a=new Element('DIV','a'),b=new Element('DIV','b');a.childNodes=[text('a')];b.childNodes=[text('b')];comp.childNodes=[a,b];assert.equal(run('composer').value,'a\nb');
function message(role,id,bodyText=''){const el=new Element('DIV',id,{'data-role':role});el.parentElement=body;el.childNodes=[text(bodyText)];el.innerText=bodyText;return el}
messages=[message('user','old-user','old prompt'),message('assistant','old-assistant')];run('mark');run('bindPrompt','new prompt');
messages=[message('user','old-user'),message('assistant','old-assistant')];
let oldImage=new Element('IMG','old-image');oldImage.complete=true;oldImage.naturalWidth=1536;oldImage.src='old';oldImage.parentElement=messages[1];messages[1].childNodes=[oldImage];images=[oldImage];
assert.equal(run('results',{}).images.length,0,'React rerender must not turn historical messages into results');
messages.push(message('user','new-user','new prompt'),message('assistant','new-assistant'));
let newImage=new Element('IMG','new-image',{'data-testid':'mdbox_image'});newImage.complete=true;newImage.naturalWidth=32;newImage.src='new';newImage.parentElement=messages[3];messages[3].childNodes=[newImage];images.push(newImage);
assert.deepEqual(run('results',{}).images.map(x=>x.selector),['img[data-testid="mdbox_image"]'],'only new assistant after new user; no image-size ownership rule');
const contentWrapper=new Element('DIV','content-wrapper',{'data-testid':'message_image_content'}),generatedCard=new Element('DIV','generated-card',{'data-testid':'mdbox_image'}),picture=new Element('PICTURE','picture'),fullImage=new Element('IMG','full-image'),thumbImage=new Element('IMG','thumb-image');
contentWrapper.parentElement=messages[3];generatedCard.parentElement=contentWrapper;picture.parentElement=generatedCard;fullImage.parentElement=picture;thumbImage.parentElement=generatedCard;
fullImage.complete=thumbImage.complete=true;fullImage.naturalWidth=fullImage.naturalHeight=1536;thumbImage.naturalWidth=thumbImage.naturalHeight=384;fullImage.src='generated-full';thumbImage.src='generated-thumbnail';
picture.childNodes=[fullImage];generatedCard.childNodes=[picture,thumbImage];contentWrapper.childNodes=[generatedCard];messages[3].childNodes=[contentWrapper];
const priorImages=images;images=[oldImage,fullImage,thumbImage];
assert.deepEqual(run('results',{}).images.map(x=>x.selector),['div[data-testid="mdbox_image"]'],'a generated card inside generic message_image_content is one SDK-clickable item despite full+thumbnail IMG representations');
messages[3].childNodes=[newImage];images=priorImages;
newImage.naturalHeight=32;
assert.ok(run('init').diagnostic.some(line=>line.includes('natural=32x32')&&line.includes('baselineNode=false')),'initialization reports existing assistant image state without uploading or sending');
const siblingMedia=new Element('DIV','sibling-media',{'data-testid':'mdbox_image'}),siblingImage=new Element('IMG','sibling-generated');
siblingMedia.parentElement=body;siblingImage.parentElement=siblingMedia;siblingImage.complete=true;siblingImage.naturalWidth=1536;siblingImage.naturalHeight=1536;siblingMedia.childNodes=[siblingImage];body.childNodes.push(siblingMedia);images.push(siblingImage);
const mediaProbe=run('init').diagnostic;
assert.ok(mediaProbe.some(line=>line.startsWith('mediaProbe#0')&&line.includes('followingLatestUser=true')),'explicit MDBOX outside the assistant role is probed with document ordering');
assert.ok(mediaProbe.some(line=>line.startsWith('mediaProbe#0 role=none')),'sibling media probe exposes absence of an ancestor message role');
body.childNodes.pop();images.pop();
// No stable ids: an old repeated prompt and changing old assistant toolbar
// must not prevent anchoring the additional occurrence of this task prompt.
images=[oldImage];messages=[message('user','','new prompt'),message('assistant','','old answer')];run('mark');run('bindPrompt','new prompt');
messages=[message('user','','new prompt'),message('assistant','','updated old answer')];
messages[1].childNodes.push(oldImage);oldImage.parentElement=messages[1];
assert.equal(run('results',{}).images.length,0,'rerender and changed historical content do not add a task user occurrence');
messages.push(message('user','','prefix new prompt suffix'),message('assistant','','unrelated answer'));
assert.ok(run('results',{}).summary.includes('anchor=false'),'a substring inside a longer plain message is not a prompt match');
messages.splice(-2);
messages.push(message('user','','new prompt'),message('assistant','','new answer'));
const promptBody=new Element('DIV','prompt-body'),spanTime=new Element('SPAN','message-time'),fileName=new Element('SPAN','attachment-name');
promptBody.childNodes=[text('new prompt')];spanTime.childNodes=[text('今天20:20')];fileName.childNodes=[text('测试色彩图.png')];
[promptBody,spanTime,fileName].forEach(node=>node.parentElement=messages[2]);messages[2].childNodes=[promptBody,spanTime,fileName];
newImage.parentElement=messages[3];messages[3].childNodes.push(newImage);images.push(newImage);
const sourcePreview=new Element('IMG','source-preview',{'data-testid':'message_image_content'});sourcePreview.complete=true;sourcePreview.naturalWidth=1536;sourcePreview.src='compressed-source';sourcePreview.parentElement=messages[2];messages[2].childNodes.push(sourcePreview);images.push(sourcePreview);
assert.equal(run('results',{}).images.length,1,'exact prompt body within a user wrapper with SPAN time/attachment name anchors current assistant; source preview excluded');
messages.splice(0,2);
assert.equal(run('results',{}).images.length,1,'virtualizing old matching users must not clear the already confirmed task anchor');
messages[0].isConnected=false;messages[0]=message('user','','new prompt');
assert.equal(run('results',{}).images.length,1,'a confirmed task may relocate its replaced anchor to the exact last user');
run('mark');run('bindPrompt','new prompt');
assert.ok(run('results',{}).summary.includes('anchor=false'),'Mark must clear confirmation; the last historical repeated prompt cannot anchor a new task');
const knownTail=message('assistant','','old tail');messages=[message('user','','new prompt'),knownTail];images=[oldImage];run('mark');run('bindPrompt','new prompt');
messages=[knownTail,message('user','','new prompt'),message('assistant','','current answer')];
newImage.parentElement=messages[2];messages[2].childNodes.push(newImage);images.push(newImage);
assert.equal(run('results',{}).images.length,1,'a known connected tail proves an append even when virtualization and insertion keep the prompt count unchanged');
run('mark');run('bindPrompt','new prompt');
messages=[message('assistant','','old tail'),message('user','','new prompt'),message('assistant','','current answer')];
assert.ok(run('results',{}).summary.includes('anchor=false'),'full old-node replacement provides no append evidence and must not guess from the last repeated prompt');
const previewDialog=new Element('DIV','preview-dialog',{'role':'dialog'}),previewClose=new Element('BUTTON','preview-close',{'aria-label':'关闭','title':'关闭'});
previewClose.innerText='×';previewClose.parentElement=previewDialog;previewDialog.childNodes=[previewClose];previewDialog.parentElement=body;body.childNodes.push(previewDialog);
assert.equal(run('results',{preview:'current-image'}).close,'button[aria-label="关闭"]','duplicate aria/title plus icon text must still expose close action');
body.childNodes.pop();
const editorRoot=new Element('DIV','actual-image-editor'),saveButton=new Element('BUTTON','save-original',{'aria-label':'保存'});
editorRoot.innerText='创建画布 标记改图 抠图 擦除 扩图 保存';saveButton.innerText='保存';saveButton.parentElement=editorRoot;editorRoot.parentElement=body;editorRoot.childNodes=[saveButton];body.childNodes.push(editorRoot);
assert.equal(run('results',{}).downloads.length,0,'global Save is never a result before this task image has been opened');
assert.equal(run('results',{preview:'current-image'}).downloads[0].selector,'button[aria-label="保存"]','the observed semantic image editor exposes its exact Save as a download without role=dialog');
assert.equal(run('preview').preview,true);
assert.equal(run('clean').preview,true,'only this explicit leftover image editor may be escaped during clean');
editorRoot.isConnected=saveButton.isConnected=false;body.childNodes.pop();
assert.equal(run('preview').preview,false,'closing is verified from preview disappearance');
editorRoot.isConnected=saveButton.isConnected=true;editorRoot.innerText='登录 验证码 保存';body.childNodes.push(editorRoot);
assert.equal(run('clean').preview,false,'a generic login/CAPTCHA Save is not the image editor');
body.childNodes.pop();editorRoot.isConnected=saveButton.isConnected=false;
const surface=new Element('DIV','surface',{'class':'guidance-input-surface'}),entry=new Element('BUTTON','unstable-id',{'data-testid':'upload_file_button'});
surface.parentElement=body;comp.parentElement=surface;entry.parentElement=surface;surface.childNodes=[comp,entry];body.childNodes=[surface];hit=entry;
assert.deepEqual(run('uploadStart').open,['button[data-testid="upload_file_button"]','text="上传文件或图片"'],'Doubao uses stable entry then its observed second-level upload option');
const wrapper=new Element('DIV','moving-layout');wrapper.parentElement=surface;entry.parentElement=wrapper;wrapper.childNodes=[entry];surface.childNodes=[wrapper,comp];
assert.deepEqual(run('uploadStart').open,['button[data-testid="upload_file_button"]','text="上传文件或图片"'],'layout movement must preserve semantic selector');
const menu=new Element('DIV','upload-menu',{'role':'listbox'}),item=new Element('DIV','upload-option',{'role':'option'});
menu.parentElement=body;item.parentElement=menu;item.innerText='上传文件或图片';menu.childNodes=[item];body.childNodes.push(menu);
assert.deepEqual(run('uploadStart').open,['text="上传文件或图片"'],'an already open upload menu must not toggle its entry closed');
menu.hidden=true;
assert.deepEqual(run('uploadStart').open,['button[data-testid="upload_file_button"]','text="上传文件或图片"'],'a hidden stale menu must not skip opening the current menu');
body.childNodes.pop();
const twin=new Element('BUTTON','duplicate',{'data-testid':'upload_file_button'});twin.parentElement=body;body.childNodes.push(twin);
assert.deepEqual(run('uploadStart').open,['#unstable-id','text="上传文件或图片"'],'nonunique attributes must not select an unrelated control');
body.childNodes.pop();entry.attributes={'aria-label':'添加附件'};comp.childNodes=[];
const qwenMenuLabel='上传图片';
comp.childNodes=[text('用户未发送草稿')];
assert.throws(()=>run('uploadStart'),/当前输入区有未发送内容，未操作上传菜单/,'real draft text must fail before returning any upload/menu plan');
assert.equal(run('composer').value,'用户未发送草稿','draft protection must preserve the editor contents');
comp.childNodes=[text('\uFEFF'),placeholder];
assert.equal(run('uploadStart').menuLabel,qwenMenuLabel,'BOM and explicitly noneditable placeholder are semantically empty');
comp.childNodes=[];
assert.deepEqual(run('uploadStart').open,['button[aria-label="添加附件"]'],'Qwen only opens its stable entry; SDK keyboard navigation selects the menu label');
assert.equal(run('uploadStart').menuLabel,qwenMenuLabel);
const qwenMenu=new Element('DIV','qwen-upload-menu',{'role':'menu'}),documentItem=new Element('DIV','qwen-document',{'role':'menuitem'}),imageItem=new Element('DIV','qwen-image',{'role':'menuitem'});
documentItem.innerText='上传文档';imageItem.innerText='上传图片';documentItem.parentElement=imageItem.parentElement=qwenMenu;qwenMenu.childNodes=[documentItem,imageItem];qwenMenu.parentElement=body;body.childNodes.push(qwenMenu);
assert.deepEqual(run('uploadStart').open,[],'an open Qwen menu needs no entry click or DOM selector click');
assert.equal(run('uploadStart').menuLabel,qwenMenuLabel);
assert.equal(qwenMenu.querySelectorAll('[role="menuitem"]').filter(item=>item.innerText==='上传图片').length,1,'Upload Image is unique while the broader Upload label would match two actions');
const imageLabel=new Element('SPAN','image-label'),imageLimit=new Element('DIV','image-limit');
imageLabel.innerText='上传图片';imageLimit.innerText='最多上传10张，每张不超过20MB';imageLabel.parentElement=imageLimit.parentElement=imageItem;imageItem.childNodes=[imageLabel,imageLimit];imageItem.innerText='上传图片\n最多上传10张，每张不超过20MB';
const describedPlan=run('uploadStart');
assert.deepEqual(describedPlan.open,[],'a descriptive child must not hide an already open Upload Image menu');
assert.equal(describedPlan.menuLabel,qwenMenuLabel);
const imageMatches=qwenMenu.querySelectorAll('[role="menuitem"]').filter(item=>item.innerText.includes('上传图片'));
assert.deepEqual(imageMatches.map(item=>item.id),['qwen-image'],'the image label remains unique despite additional limit text and the separate Upload Document action');
assert.ok(run('upload',1).diagnostic.some(line=>line.startsWith('uploadMenu#')&&line.includes('最多上传10张')),'failure diagnostics expose only current upload-option copy');
const otherItem1=new Element('DIV','qwen-other1',{'role':'menuitem'}),otherItem2=new Element('DIV','qwen-other2',{'role':'menuitem'});
otherItem1.innerText='其它入口一';otherItem2.innerText='其它入口二';otherItem1.parentElement=otherItem2.parentElement=qwenMenu;qwenMenu.childNodes.push(otherItem1,otherItem2);
for(const parentRole of ['dialog','listbox']){
 qwenMenu.attributes.role=parentRole;
 assert.equal(qwenMenu.querySelectorAll('[role="menuitem"]').length,4);
 assert.deepEqual(run('uploadStart').open,[],'visible upload image items need only SDK keyboard selection regardless of parent role');
 assert.equal(run('uploadStart').menuLabel,qwenMenuLabel);
 assert.equal(qwenMenu.querySelectorAll('[role="menuitem"]').filter(item=>item.innerText.includes('上传图片')).length,1,'only the business image item matches among four actions');
 assert.ok(run('upload',1).diagnostic.some(line=>line.startsWith('uploadMenu#')&&line.includes('qwen-image')),'item diagnostics do not depend on parent role');
}
qwenMenu.zeroRect=true;
assert.deepEqual(run('uploadStart').open,[],'a zero-rect/display-contents menu still has visible actionable items and must not be toggled closed');
assert.ok(run('uploadStart').summary.includes('plannedSteps=0;menuAlreadyOpen=true;menuLabel=上传图片'),'the normal log plan distinguishes keyboard-only selection from an entry toggle');
assert.ok(run('upload',1).diagnostic.some(line=>line.startsWith('uploadMenu#')&&line.includes('qwen-image')),'visible children of a zero-rect menu remain in diagnostics');
qwenMenu.zeroRect=false;
qwenMenu.hidden=true;
assert.deepEqual(run('uploadStart').open,['button[aria-label="添加附件"]'],'a stale hidden Qwen menu must be reopened');
assert.ok(run('uploadStart').summary.includes('plannedSteps=1;menuAlreadyOpen=false;menuLabel=上传图片'),'the normal log plan records the entry-click plus SDK menu-selection path');
body.childNodes.pop();
const promotion=new Element('DIV','promotion'),cta=new Element('BUTTON','cta'),close=new Element('BUTTON','promo-close',{'aria-label':'关闭'}),loginClose=new Element('BUTTON','login-close',{'aria-label':'关闭'});
cta.innerText='立即下载电脑版';promotion.parentElement=body;cta.parentElement=promotion;close.parentElement=promotion;promotion.childNodes=[cta,close];loginClose.parentElement=body;body.childNodes.push(promotion,loginClose);
assert.equal(run('clean').promotionClose,'#promo-close','only close the specifically identified client promotion');
hit=close;
assert.ok(run('upload',1).diagnostic.some(line=>line.includes('centerHit=false')&&line.includes('promo-close')),'failure diagnostics identify an overlay hit without text/URLs');
cta.innerText='登录';
assert.equal(run('clean').promotionClose,'','never close a generic login/other close button');
// Qwen's observed message cards have no role attributes.
location.hostname='www.qianwen.com';comp.childNodes=[];
function qwenCard(kind,id,prompt){
 const card=new Element('DIV',id,{'class':kind==='user'?'chat-question-card-wrap':'chat-answers-card-wrap'});
 const body=new Element('DIV',id+'-body',{'class':kind==='user'?'message-card-wrap question':'answer-body'});
 const content=new Element('DIV',id+'-text');content.childNodes=[text(prompt)];content.innerText=prompt;content.parentElement=body;body.childNodes=[content];body.parentElement=card;card.childNodes=[body];card.parentElement=document.body;return card;
}
function qwenImage(card,id,src){
 let content=card.childNodes[0];
 if(card.getAttribute('class')==='chat-answers-card-wrap'){
  const markdownWrap=new Element('DIV',id+'-markdown',{'class':'markdown-pc-special-class'}),markdown=new Element('DIV','qk-markdown-react',{'class':'qk-markdown'});
  markdownWrap.parentElement=content;markdown.parentElement=markdownWrap;markdownWrap.childNodes=[markdown];content.childNodes.push(markdownWrap);content=markdown;
 }
 const cardWrap=new Element('DIV',id+'-card',{'class':'image-card-wrapper'}),single=new Element('DIV',id+'-single',{'class':'single-image'}),img=new Element('IMG',id,{'class':'single-image-img'});
 cardWrap.parentElement=content;single.parentElement=cardWrap;img.parentElement=single;img.src=src;img.complete=true;img.naturalWidth=img.naturalHeight=1536;single.childNodes=[img];cardWrap.childNodes=[single];content.childNodes.push(cardWrap);return img;
}
messages=[qwenCard('user','qwen-old-user','old prompt'),qwenCard('assistant','qwen-old-assistant','old answer')];
const oldQwenSource=qwenImage(messages[0],'old-qwen-source','old-source'),oldQwenResult=qwenImage(messages[1],'old-qwen-result','old-result');images=[oldQwenSource,oldQwenResult];
assert.ok(run('mark').diagnostic[0].includes('assistant=1;user=1'),'Qwen uses only the observed question/answer card fallback when roles are absent');
run('bindPrompt','current exact prompt');
assert.equal(run('results',{}).images.length,0,'Qwen fallback never recovers old historical result images');
messages.push(qwenCard('user','qwen-current-user','current exact prompt'),qwenCard('assistant','qwen-current-assistant','new answer'));
const qwenSource=qwenImage(messages[2],'qwen-source','current-source'),qwenResult=qwenImage(messages[3],'qwen-generated','current-generated');images.push(qwenSource,qwenResult);
assert.deepEqual(run('results',{}).images.map(item=>item.selector),['#qwen-generated'],'the exact prompt anchors only the current Qwen assistant image, excluding the user source and old history');
const toolbar=new Element('DIV','qwen-operation-toolbar');toolbar.parentElement=messages[3];messages[3].childNodes.push(toolbar);
const operationIcons=[0,1].map(index=>{const span=new Element('SPAN','qwen-operation-'+index),icon=new Element('IMG','qwen-operation-icon-'+index,{'class':'size-4 object-contain pointer-events-none absolute inset-0'});span.parentElement=toolbar;icon.parentElement=span;span.childNodes=[icon];toolbar.childNodes.push(span);icon.complete=true;icon.naturalWidth=icon.naturalHeight=72;icon.src='operation-icon-'+index;return icon});
images.push(...operationIcons);qwenResult.complete=false;qwenResult.naturalWidth=qwenResult.naturalHeight=0;
assert.equal(run('results',{}).images.length,0,'ready operation icons outside Qwen markdown must not substitute for a still-loading real result');
qwenResult.naturalWidth=qwenResult.naturalHeight=1536;
assert.equal(run('results',{}).images.length,0,'a Qwen body image must finish loading before it can be selected');
qwenResult.complete=true;
assert.deepEqual(run('results',{}).images.map(item=>item.selector),['#qwen-generated'],'asynchronous body-image readiness yields only the current result, excluding toolbar icons, source and old history');
const qwenMarkdown=qwenResult.closest('#qk-markdown-react,.markdown-pc-special-class');qwenMarkdown.id='markdown-content';
assert.deepEqual(run('results',{}).images.map(item=>item.selector),['#qwen-generated'],'the observed markdown wrapper also scopes a result without the inner markdown id');
qwenMarkdown.id='qk-markdown-react';
const generationCard=new Element('DIV','card_card_ai_generate_image_96992_16551_96992_82203'),loadingImage=new Element('IMG','qwen-loading',{'alt':'loading'});
generationCard.parentElement=messages[3];loadingImage.parentElement=generationCard;generationCard.childNodes=[loadingImage];messages[3].childNodes.push(generationCard);
loadingImage.src='loading-placeholder';loadingImage.complete=true;loadingImage.naturalWidth=loadingImage.naturalHeight=512;images.push(loadingImage);qwenResult.hidden=true;
let pendingQwen=run('results',{});
assert.equal(pendingQwen.busy,true,'the observed Qwen generation-card loading image is busy even without progressbar or generating text');
assert.equal(pendingQwen.images.length,0,'a fully loaded, image-sized loading placeholder must never be a result candidate');
qwenResult.hidden=false;
assert.equal(run('results',{}).busy,true,'an underlying complete image cannot bypass a visible generation loading overlay');
loadingImage.hidden=true;
const completedQwen=run('results',{});
assert.equal(completedQwen.busy,false,'a hidden stale loading image no longer blocks a finished Qwen result');
assert.deepEqual(completedQwen.images.map(item=>item.selector),['#qwen-generated'],'only the real completed image enters the existing SDK download flow');
loadingImage.hidden=false;generationCard.parentElement=messages[1];messages[3].childNodes.pop();messages[1].childNodes.push(generationCard);
assert.equal(run('results',{}).busy,false,'loading in a historical assistant must not block the current task');
generationCard.parentElement=messages[3];messages[1].childNodes.pop();messages[3].childNodes.push(generationCard);generationCard.id='ordinary-image-card';
assert.equal(run('results',{}).busy,false,'the loading marker is limited to the observed Qwen generation-card identity');
generationCard.id='card_card_ai_generate_image_96992_16551_96992_82203';loadingImage.isConnected=false;generationCard.childNodes=[];images.pop();
assert.equal(run('results',{}).busy,false,'removing the placeholder releases the generation wait');
const qwenViewer=new Element('DIV','qwen-image-viewer',{'class':'root-bJIrUF !fixed left-0 top-0'}),backToChat=new Element('BUTTON','back-to-chat',{'aria-label':'回到对话'}),viewerClose=new Element('BUTTON','qwen-viewer-close',{'aria-label':'关闭'}),viewerImage=new Element('IMG','qwen-viewer-main'),imageList=new Element('DIV','qwen-history-list',{'role':'listbox','aria-label':'图片列表'}),historicalThumb=new Element('IMG','qwen-history-thumb'),historicalDownload=new Element('SPAN','qwen-history-download'),viewerToolbar=new Element('DIV','qwen-viewer-toolbar');
qwenViewer.parentElement=body;qwenViewer.childNodes=[backToChat,viewerClose,viewerImage,imageList,viewerToolbar];qwenViewer.childNodes.forEach(node=>node.parentElement=qwenViewer);historicalThumb.parentElement=historicalDownload.parentElement=imageList;historicalDownload.innerText='下载';imageList.childNodes=[historicalThumb,historicalDownload];
const addToChat=new Element('SPAN','qwen-add-to-chat'),downloadWrap=new Element('DIV','qwen-download-wrap'),viewerDownload=new Element('SPAN','qwen-download'),ultra4k=new Element('SPAN','qwen-ultra-4k');
addToChat.innerText='添加到对话';downloadWrap.innerText=viewerDownload.innerText='下载';ultra4k.innerText='4K超清';viewerToolbar.childNodes=[addToChat,downloadWrap,ultra4k];viewerToolbar.childNodes.forEach(node=>node.parentElement=viewerToolbar);viewerDownload.parentElement=downloadWrap;downloadWrap.childNodes=[viewerDownload];body.childNodes.push(qwenViewer);images.push(viewerImage,historicalThumb);
assert.equal(run('results',{}).downloads.length,0,'global role-less viewer download is not accepted before opening this task image');
let qwenPreview=run('results',{preview:'qwen-generated'});
assert.deepEqual(qwenPreview.downloads.map(item=>item.selector),['#qwen-download'],'the proven Qwen viewer exposes its one exact leaf Download, excluding the parent wrapper, 4K and historical sidebar');
assert.equal(qwenPreview.close,'#qwen-viewer-close','only the uniquely identified viewer Close is offered');
assert.equal(run('preview').preview,true);assert.equal(run('preview').close,'#qwen-viewer-close');
assert.equal(run('clean').preview,true,'recognized leftover Qwen editor can be closed before the next task');
const duplicateDownload=new Element('SPAN','qwen-download-duplicate');duplicateDownload.innerText='下载';duplicateDownload.parentElement=viewerToolbar;viewerToolbar.childNodes.push(duplicateDownload);
assert.equal(run('results',{preview:'qwen-generated'}).downloads.length,0,'ambiguous duplicate Download labels must not choose an arbitrary control');
viewerToolbar.childNodes.pop();ultra4k.innerText='超清';
assert.equal(run('results',{preview:'qwen-generated'}).downloads.length,0,'without the observed 4K editor identity a generic download is not trusted');
assert.equal(run('clean').preview,false,'an unrecognized generic panel is not cleaned as an image editor');
ultra4k.innerText='4K超清';location.hostname='www.doubao.com';
assert.equal(run('clean').preview,false,'Qwen viewer identity must not be used on another host');
location.hostname='www.qianwen.com';qwenViewer.hidden=true;
assert.equal(run('preview').preview,false,'hidden or closed Qwen viewer no longer blocks the next task');
assert.equal(run('clean').preview,false);body.childNodes.pop();images.splice(-2);qwenViewer.isConnected=false;
const roleUser=message('user','role-user','role prompt'),roleAssistant=message('assistant','role-assistant','answer'),nestedQuestion=qwenCard('user','nested-question','role prompt'),nestedAnswer=qwenCard('assistant','nested-answer','answer');
nestedQuestion.parentElement=roleUser;nestedAnswer.parentElement=roleAssistant;roleUser.childNodes=[nestedQuestion];roleAssistant.childNodes=[nestedAnswer];messages=[roleUser,nestedQuestion,roleAssistant,nestedAnswer];images=[];
assert.ok(run('mark').diagnostic[0].includes('assistant=1;user=1'),'role-backed pages must not also count nested Qwen fallback cards');
`
	cmd := exec.Command(node, "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated DOM fixture: %v\n%s", err, out)
	}
}
