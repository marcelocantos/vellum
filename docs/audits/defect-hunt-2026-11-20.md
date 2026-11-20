# Defect hunt 2026-11-20

Read-only hunt. Nothing in this branch except this report. Reproductions ran from `/tmp` against this worktree, with `httptest` on an ephemeral port and a private cache directory. The installed vellum, the `vellum-view` services, the logged-in pasteboard, and remote services were not touched.

Each reproduction below fails on this tree. Paste it into the package it names. `Render` is called with `Options{Defer: true}` so no KaTeX or Mermaid subprocess runs.

## Confirmed

### 1. Opening a document never lets that document read another file

Severity: high. This is a file the viewer was not pointed at. The page and the file server share an origin, so a document can read any path the process can read.

`html.WithUnsafe` (`convert/convert.go:85`) leaves a raw `<script>` in the page. `urlPathToFS` (`viewer/server.go:391`) turns the URL path into an absolute filesystem path and does not confine it to a root. `serveFile` (`viewer/server.go:552`) opens that path. `localRequestAllowed` (`viewer/server.go:239`) allows `Sec-Fetch-Site: same-origin`, which is what the page's own `fetch` sends. `requireMarkdownFile` (`viewer/watch.go:137`) uses `os.Stat`, which follows symlinks, and `handleEdit` GET (`viewer/edit.go:134`) returns the target bytes. The homepage tree skips symlink entries (`viewer/home.go:213`); the file and edit handlers do not.

A document at `docs/note.md` containing `<script>` fetched `../secret.txt` and received `SECRET-BYTES`. `docs/alias.md` pointing at that file rendered the secret, and `GET /_vellum/edit` returned the secret bytes. The homepage would not have listed the symlink.

Smallest fix: give the server an allowlist of roots (the open document's directory and the homepage roots) and refuse a path, after `EvalSymlinks`, that is outside them. Render Markdown without raw HTML, or send a CSP that blocks inline script.

```go
func TestViewedDocumentCannotReadOutsideItsDirectory(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET-BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(docs, "note.md")
	body := "<script>window.__vellum = 1</script>\n"
	if err := os.WriteFile(md, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(docs, "alias.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(root, "cache")}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	page, err := http.Get(ts.URL + pathURL(md))
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if strings.Contains(string(pageBody), "<script>window.__vellum = 1</script>") {
		t.Fatal("raw script from the document was served in the page")
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+pathURL(secret), nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 && strings.Contains(string(got), "SECRET-BYTES") {
		t.Fatal("same-origin fetch read a file outside the document directory")
	}
	edit, err := http.Get(ts.URL + "/_vellum/edit?path=" + url.QueryEscape(link))
	if err != nil {
		t.Fatal(err)
	}
	editBody, _ := io.ReadAll(edit.Body)
	edit.Body.Close()
	if string(editBody) == "SECRET-BYTES" {
		t.Fatal("edit GET followed a .md symlink")
	}
}
```

### 2. A Markdown link never becomes a javascript: URL

Severity: high. In the viewer this runs as a same-origin click, which can then do what defect 1 allows. The existing TOC test only asserts the absence of `javascript:` on a document that contains no such link (`convert/toc_test.go:204`).

Goldmark emits the destination as an `href` with no scheme allowlist. `Render` of `[go](javascript:alert(1))` produced `<a href="javascript:alert(1)">go</a>`.

Smallest fix: when rendering links, allow `http`, `https`, `mailto`, and relative URLs, and drop the rest.

```go
func TestRenderDropsJavascriptLink(t *testing.T) {
	html, _, err := Render(context.Background(), []byte("[go](javascript:alert(1))\n"), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(html), "javascript:") {
		t.Fatalf("javascript URL in convert HTML: %s", html[strings.Index(html, "<body"):])
	}
}
```

### 3. Toggling a checkbox never changes a different line

Severity: high. The handler writes the user's document and reports success while changing a line the user did not click, including a line inside a code fence.

The page posts `data-task-index` from the checkbox goldmark rendered (`viewer/chrome.js:1760` and `:450`). `toggleTaskMarker` (`viewer/task.go:69`) counts a different set: `(?m)^(\s*[-*+]\s+\[)([ xX])(\])` at `viewer/task.go:21`. That regex matches markers inside fences and misses a marker indented by a blockquote. `annotateTaskCheckboxes` numbers only real checkboxes.

Clicking index 0 on a document whose first marker is inside a fence changed the fence and left `- [ ] real task` untouched. On `> - [ ] quoted task` followed by `- [ ] real task`, the page exposed two checkboxes and index 0 changed the second item. The quote stayed `- [ ]`.

Smallest fix: when rendering, record each task item's byte offset and toggle that offset. Stop scanning the source with a line regex.

```go
func TestToggleHitsTheCheckboxThatWasClicked(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "tasks.md")
	src := "```\n- [ ] example in code\n```\n\n- [ ] real task\n"
	if err := os.WriteFile(md, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/_vellum/task-toggle?path="+url.QueryEscape(md)+"&index=0", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got, _ := os.ReadFile(md)
	if strings.Contains(string(got), "[x] example in code") || !strings.Contains(string(got), "[x] real task") {
		t.Fatalf("index 0 rewrote the wrong item:\n%s", got)
	}
}
```

The blockquote case is the same call with source `"> - [ ] quoted task\n\n- [ ] real task\n"`. Index 0 must change the quoted item only. Today it changes `real task`.

### 4. A code block never has its text replaced by a math placeholder

Severity: high. The code block's source is gone and nothing is rendered in its place. The reader sees an HTML comment instead of the code.

`fencedCodeRe` and `inlineCodeRe` (`convert/katex.go:74` and `:76`) protect only a backtick fence at column 0 and a single-backtick span. CommonMark also treats a fence indented by one to three spaces, a `~~~` fence, and a four-space indented code block as code. `Extract` (`convert/katex.go:107`) then replaces `$…$` inside them. Goldmark escapes the placeholder because it is code text, so `deferAll` (`convert/fragment.go:144`) never substitutes it.

Observed bodies:

- `" ```\n $E = mc^2$\n ```\n"` rendered `<pre><code>&lt;!--MATH:0--&gt;\n</code></pre>`
- `"~~~\n$E = mc^2$\n~~~\n"` rendered the same
- `"    price is $5 and $10\n"` rendered `<pre><code>price is &lt;!--MATH:0--&gt;10\n</code></pre>`

`adf/math.go:20` uses the same two regular expressions before rewriting math into fences, so a fenced block indented by a space is unprotected on that path too.

Smallest fix: mask code the way CommonMark defines it (fences of `` ` `` or `~`, indented up to three spaces, indented code blocks, and backtick spans whose delimiter length matches) before scanning for `$`.

```go
func TestMathLeavesIndentedFenceAlone(t *testing.T) {
	src := " ```\n $E = mc^2$\n ```\n"
	html, _, err := Render(context.Background(), []byte(src), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "$E = mc^2$") || strings.Contains(html, "MATH:") {
		t.Fatalf("code block was rewritten:\n%s", html[strings.Index(html, "<body"):])
	}
}
```

Repeat with `"~~~\n$E = mc^2$\n~~~\n"` and `"    price is $5 and $10\n"`. Both must keep the dollars and must not contain `MATH:`.

### 5. Inline math never pulls a neighbouring table cell out of the table

Severity: high. A cell's text leaves the table. The ADF package documents that math becomes a `latex` code block (`adf/doc.go`). It does not document that the rewrite deletes the row the expression sat in.

`extractMathAsLatexFences` (`adf/math.go:48`) replaces `$x$` with a newline, a fence, and a newline. Goldmark then ends the table. For `| a | $x$ | b |\n| --- | --- | --- |\n| 1 | 2 | 3 |\n` the ADF document was a paragraph `| a |`, then a `codeBlock` of `x`, then a table whose header row starts at `b` and whose first header cells are empty. `a` is not a cell.

Smallest fix: rewrite inline math to an inline code span (`` `$x$` `` or a code mark) so the table row stays one line. Keep the multiline fence for `$$` display blocks only.

```go
func TestInlineMathStaysInsideItsTableCell(t *testing.T) {
	src := "| a | $x$ | b |\n| --- | --- | --- |\n| 1 | 2 | 3 |\n"
	doc, err := Convert(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	s := string(raw)
	if strings.Count(s, `"tableHeader"`) != 3 || strings.Count(s, `"tableCell"`) != 3 || !strings.Contains(s, `"text":"a"`) {
		t.Fatalf("table lost a cell: %s", s)
	}
}
```

### 6. Import never records a file outside the media directory as an asset

Severity: high. A path written in the document becomes a file the importer stats, and the rewritten Markdown names that file as if it had been extracted.

`AbsolutizeMedia` (`importer/result.go:56`) joins a relative image reference onto `mediaDir` and, if that misses, stats the path as given (`result.go:86` and `:97`). An absolute reference is cleaned and stat'd (`result.go:76`). Neither result has to stay under `mediaDir`. `![](../secret.png)` and an absolute `![](/…/secret.png)` both came back as assets, and both references were rewritten to the absolute path of `secret.png`.

Smallest fix: after `filepath.Abs` and `Clean`, keep the reference only when `filepath.Rel(mediaDir, path)` does not start with `..`. Leave anything else unchanged and do not add it to the asset list.

```go
func TestAbsolutizeMediaStaysInsideMediaDir(t *testing.T) {
	root := t.TempDir()
	media := filepath.Join(root, "media")
	if err := os.Mkdir(media, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.png")
	if err := os.WriteFile(secret, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	md := "![](../secret.png)\n\n![](" + secret + ")\n"
	out, assets, err := AbsolutizeMedia(md, media)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, secret) || len(assets) != 0 {
		t.Fatalf("outside file recorded: %s assets=%v", out, assets)
	}
}
```

### 7. Two saves of one document never drop one of the writes

Severity: medium. The file on disk loses a write that the client was told succeeded. The editor's stamp does not cover the gap between the check and the rename, and task toggle has no stamp.

`handleEdit` (`viewer/edit.go:170`) compares the stamp taken at the start of the request, then calls `writeFn` (`edit.go:179`). Two PUTs that both pass that check before either writes both return 200. With a hook that blocked inside `WriteFile` until both PUTs had arrived, both returned 200 and the file kept only `AAAA`. `handleTaskToggle` (`viewer/task.go:157`) reads, toggles one index, and writes with no version check. The same barrier on indexes 0 and 1 produced writes `- [x] one` / `- [ ] two` and `- [ ] one` / `- [x] two`; the file kept only the first of those.

Smallest fix: hold a per-path mutex across the read, the mutate, and the rename. For the editor, stat again immediately before the rename and return 409 when the stamp moved.

```go
func TestEditorPutRejectsAStampThatAlreadyRaced(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "note.md")
	if err := os.WriteFile(md, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	got, err := http.Get(ts.URL + "/_vellum/edit?path=" + url.QueryEscape(md))
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	stamp := got.Header.Get("X-Vellum-Stamp")
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	s.WriteFile = func(path string, data []byte) error {
		arrived <- struct{}{}
		<-release
		return os.WriteFile(path, data, 0o644)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, body := range []string{"AAAA\n", "BBBB\n"} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPut, ts.URL+"/_vellum/edit?path="+url.QueryEscape(md), strings.NewReader(body))
			req.Header.Set("X-Vellum-Stamp", stamp)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				codes <- 0
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}(body)
	}
	<-arrived
	<-arrived
	close(release)
	wg.Wait()
	close(codes)
	ok := 0
	for c := range codes {
		if c == 200 {
			ok++
		}
	}
	if ok == 2 {
		final, _ := os.ReadFile(md)
		t.Fatalf("both PUTs returned 200; final %q", final)
	}
}
```

### 8. A code-protection sentinel never rewrites a different span

Severity: medium. The author's code span is replaced by a different span, or an HTML comment they wrote is replaced by math. The failure is silent.

Three restorers search the document for their own placeholder text:

- `convert/katex.go:141` uses `strings.Replace` once. Source `<!--CODE:0-->` followed by `` `secret` `` restored the code span onto the comment, so the comment no longer preceded the code.
- `convert/fragment.go:146` uses `strings.Replace` once. Source `<!--MATH:0-->` followed by `$x$` rendered the math span where the comment was, and left a raw `<!--MATH:0-->` where the expression was.
- `adf/math.go:59` uses `strings.ReplaceAll`. Source `` `<!--ADFCODE:1-->` `` followed by `` `SECOND` `` became two code spans of `SECOND`. The same call with `<!--ADFCODE:0-->` and `` `secret` `` became two code spans of `secret`.

Smallest fix: protect code with sentinels that cannot occur in the input (a private-use rune plus an index), and restore from the index map. Do not search the document for the placeholder text.

```go
func TestADFCodeSentinelDoesNotDuplicate(t *testing.T) {
	doc, err := Convert("`<!--ADFCODE:1-->`\n\n`SECOND`\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	s := string(raw)
	if strings.Count(s, "SECOND") != 1 || !strings.Contains(s, "ADFCODE:1") {
		t.Fatalf("sentinel restore rewrote a code span: %s", s)
	}
}
```

### 9. A plain list item never becomes a task because a sibling is one

Severity: medium. The item's text survives, and its meaning changes: a plain line becomes an unchecked task.

`list` (`adf/convert.go:159`) sets `isTask` when any item has a checkbox, then emits every item as `taskItem` (`convert.go:189`). `- [ ] buy milk` followed by `- plain item` produced two `taskItem` nodes, the second with `state` `TODO`. There is no `listItem`.

Smallest fix: emit `taskItem` only for the items `taskCheckBox` matches, and leave the others as `listItem`. A mixed list has to be two ADF lists, because a `taskList` cannot contain a plain item.

```go
func TestMixedListKeepsThePlainItem(t *testing.T) {
	doc, err := Convert("- [ ] buy milk\n- plain item\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	s := string(raw)
	if strings.Count(s, `"taskItem"`) != 1 || !strings.Contains(s, `"listItem"`) {
		t.Fatalf("plain item became a task: %s", s)
	}
}
```

### 10. An image's alt text never drops the words inside emphasis

Severity: medium. The alt is the accessible name of the image, and here it is empty.

`imageAlt` (`adf/convert.go:525`) reads only direct `ast.Text` children. Emphasis is an `Emphasis` node, so its text is skipped. `![*hidden alt*](http://example.com/a.png)` produced a media node whose attributes were `type` and `url` only.

Smallest fix: collect text from the alt's descendants, not only its direct children.

```go
func TestImageAltKeepsEmphasis(t *testing.T) {
	doc, err := Convert("![*hidden alt*](http://example.com/a.png)\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	if !strings.Contains(string(raw), "hidden alt") {
		t.Fatalf("alt dropped: %s", raw)
	}
}
```

### 11. An import never leaves a copy of the source document in the media directory

Severity: low. The media directory is where extracted images go. The original HTML is not an image, and it stays behind as mode `0644`.

`ImportBytes` writes `source.html` (or `source.pdf`) into the media directory (`importer/importer.go:144` and `:129`) and never removes it. After importing `<p>Hello import</p>`, `source.html` was still there with that body.

Smallest fix: remove the source file after pandoc (or `ImportPDF`) returns, including on the error path.

```go
func TestImportBytesRemovesItsSourceCopy(t *testing.T) {
	dir := t.TempDir()
	if _, err := ImportBytes(context.Background(), []byte("<p>Hello import</p>"), &Options{Format: "html", MediaDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "source.html")); !os.IsNotExist(err) {
		t.Fatalf("source.html still in the media dir: %v", err)
	}
}
```

## Unconfirmed

Request-rule bypasses, retested rather than trusted. `Host: 2130706433`, `0177.0.0.1`, and `localhost.` were 403. `Origin: null` was 403. A cross-site GET of `/_vellum/edit` was 403. `[::ffff:127.0.0.1]` was 200, which is `net.IP.IsLoopback`, and the listener is loopback-only. A cross-site top-level GET of `/_vellum/pdf` was 200 with an empty `Access-Control-Allow-Origin`. That is the branch in `localRequestAllowed` for a non-state-changing navigation. The linking page cannot read the body. A PUT with `Sec-Fetch-Site: same-origin` and no `Origin` reached the stamp check (428). A browser cannot set that header.

PDF import fence. `importer/pdf.go:121` wraps pdftotext output in one ``` fence. `textutil -convert pdf` is not available here ("Invalid output format"). A hand-built PDF whose page text was `Alpha ``` fence` came back from `pdftotext -layout` as curly quotes, and the wrapper fence stayed closed (`FENCECOUNT 2`). Not shown with a real backtick run.

Deep nesting and long lines. Eight thousand nested `> ` blockquotes converted in 114ms and `json.Marshal` of the ADF returned `exceeded max depth`. Depths 32, 100, 500, and 2000 kept the word `leaf`. A line of two million `a` characters rendered in 75ms. No hang. `flightGroup.do` (`viewer/lazy.go:149`) never closes `done` if `fn` panics, so a later caller of that key waits forever. No input was found that panics the renderer.

`fragmentHTML` (`viewer/lazy.go:246`) returns a cached fragment before it checks that the id is in the requested file. The id is a content hash, so this replays a fragment the caller already named. Not run as its own test, and it is not a path leak. `renderMarkdown` (`viewer/server.go:535`) writes a fixed `cachePath+".tmp"`. Two renders of the same document can tear that file. Not reproduced.

These were suspected and did not drop content. Invalid UTF-8 `before \xff after 日本語` kept `before`, `after`, and `日本語` in both HTML and ADF. An ADF footnote's body was still in the document, as ordinary text, which is the residue in `adf/doc.go`. A paragraph after `<div>raw block</div>` was still in the document; the raw block itself was absent, which is the same residue. `***both***` kept both `em` and `strong`. A double-backtick span kept `$E = mc^2$`. One Markdown document taken through HTML and back through pandoc kept the link target, `日本語`, both table cells, the code block, and the footnote body.

There is no map from a document to a Confluence page id. `adf` is a library and is not a `convert.Run` sink, so nothing in this tree publishes a document to a remote page. PDF writing goes through `renderPDFHTML` and `writePDF` once.

## Coverage

Read for this hunt: `viewer/server.go`, `viewer/edit.go`, `viewer/task.go`, `viewer/home.go`, `viewer/links.go`, `viewer/lazy.go`, `viewer/watch.go` (the request and stamp parts), `viewer/chrome.js` (the task-toggle call), `convert/convert.go`, `convert/katex.go`, `convert/fragment.go`, `convert/mermaid.go`, `convert/run.go` (ingest, the inferred-text check, batch errors), `convert/style.go`, `adf/convert.go`, `adf/math.go`, `adf/doc.go`, `importer/importer.go`, `importer/pdf.go`, `importer/result.go`, and the request-boundary tests in `viewer/viewer_test.go`. `viewer/watch_darwin.go` was read in the same hunt and no bug in it was confirmed.

Not read line by line: `clipboard/` (the live pasteboard was out of scope), `cmd/vellum/main.go`, `convert/pptx.go`, the WeasyPrint backend, `internal/pandoc`, `mcp/server.go` past the batch `IsError` flag, and the CodeMirror editor. A partial MCP batch sets `IsError` only when every item failed, and the message still lists `Errors`; that was read and is not silent.
