# Render boundaries: what a document can cause, by entry point

Analysis of `master` at `c47e428` (branch `filler/analysis-2026-11-22`),
2026-11-23. Read-only: no product code or test changed. Reproductions ran
against a tree build (`go build ./cmd/vellum`) with a private `HOME` and
`XDG_CONFIG_HOME`, `VELLUM_VIEW_ADDR` pointed at a dead port, and
`PUPPETEER_CACHE_DIR` at the user's Puppeteer cache (read only, so mmdc
finds its browser). The tree viewer ran on a random free loopback port and
was stopped afterwards. A request recorder on another random loopback port
stood in for "the network": every request in this report that reached it
would, with a real host name, have left the machine. The installed viewers
(18742, 18744), their LaunchAgents and the Homebrew install were not
touched. Tools and the reproduction driver are in
`docs/analysis/render-boundaries/`.

**Status: complete for this run.**
`python3 -I docs/analysis/render-boundaries/repro.py <vellum> <scratch dir>`
prints REPRODUCED for D1, D2, D3, D4, D5 and D7 on `c47e428` (run
2026-11-23, about 4 s). D6, D8 and the V1 battery were run in headless
Chrome with `browse.cjs` and `probe2.cjs` (the Puppeteer inside mermaid-cli, not the user's
browser).

**Answer in one paragraph.** In the browser, a document is well contained:
none of 14 script and navigation vectors ran, and its page loads only
images it names (from any host, by design). The holes are around it. The
CLI and MCP paths trust the document completely: a PDF conversion attaches
any readable file to the PDF (D1), a pptx embeds any file byte for byte
(D2), and both reach the network and can get the viewer to serve a folder
(D3). That last grant is wider than the code's comment says: a name one
level under a filesystem root, such as `/Users/x.md`, serves the whole
tree, home folder included (D7). Inside the viewer, every granted folder
is open to every document, not only to its own (D5). Import copies local
files the content names and splices in fetched iframe pages (D4). A
document can take over the viewer's consent, editor and lightbox elements
by id (D6), and one comment can stop the viewer's own script (D8).

## 1. The system, from the code

A document enters vellum four ways. Each picks a different renderer chain
and a different trust posture.

| Entry | Code | Markdown → HTML | Sanitised | Fragments (math, Mermaid) | Sink engines and their reach |
|---|---|---|---|---|---|
| Viewer page (browser GET of `/<abs path>.md`) | `viewer/server.go` `handlePath` → `serveMarkdown` → `cachedHTML` → `convert.Render` (`Defer`) | goldmark, `html.WithUnsafe` | yes: `safehtml.Document` (allow-list), then a nonce CSP (`viewer/csp.go`) | deferred; `/_vellum/fragment` renders on demand, output through `safehtml.Rendered` | the user's browser: `img-src * data: blob:`, `style-src 'self' 'unsafe-inline' https:`, `font-src 'self' data: https:`, `connect-src 'self' ws://<host>`, script by nonce only |
| Viewer actions (PDF, clipboard, reveal, task toggle, edit) | `viewer/actions.go`, `task.go`, `edit.go` | as the page | yes | rendered now, scrubbed | PDF: `InlineResources` (local via `readServed`, remote via `viewer/fetch.go`) then WeasyPrint `--allowed-protocols=data` / Prince `--no-network --no-local-files`; clipboard: AppKit (fetches nothing) or pandoc `--sandbox` |
| CLI `vellum convert` | `cmd/vellum/main.go` → `convert.Run` (`convert/run.go`) | goldmark, `WithUnsafe` | **no** | rendered now (Mermaid confined, KaTeX `trust` off) | PDF: `WritePDF`, engine with `--base-url=file://<doc dir>/` and full reach; RTF: pandoc `HTMLToRTF` with `--resource-path`; pptx: pandoc with `--resource-path`; HTML: the raw page is written out |
| MCP `convert` (daemon `/mcp`, or `vellum --mcp`) | `mcp/server.go` → `convert.Run` | as CLI | **no** | as CLI | as CLI; `from: content` has no base directory, so relative names resolve against the output file's folder (`WritePDF`, `convert/convert.go:166`) |
| Import (rich text, clipboard, docx, HTML, PDF) | `importer/importer.go`, `convert/run.go` `ingest` | pandoc reader → gfm | no | n/a | pandoc `--extract-media=<media dir>` (no `--sandbox`); Poppler for PDF |

The viewer serves files only inside *served roots*: `Server.Roots` plus a
folder granted when a Markdown document is opened "by the user", which
`openedByUser` (`viewer/roots.go:68`) defines as a request with
`Sec-Fetch-Site: none` **or with no fetch metadata at all**. The granted
folder is the top of the git work tree the document is in, or else its own
folder (`rootFor`). Grants are one global list for the whole server, not
per document, persisted in `<cache>/roots.json` (64, most recent first).
Every path-taking route resolves symlinks and opens through `os.Root`.

Long-lived state a document can influence: the view cache (rendered
shells, fragments by content id, PDFs keyed by path and the source's
stamp), `roots.json`, the MRU list behind the home page, and the import
cache (`~/Library/Caches/vellum/import/<hash>/`).

## 2. Invariants

Status: *holds (argument)*, *holds (tested)*, *violated (reproduced)*,
*by design* (the code chooses this; listed because it crosses the boundary
the question asks about), *unknown*.

| ID | Invariant | Where maintained | How it could break | Status |
|---|---|---|---|---|
| V1 | A document shown in the viewer runs no script of its author's in the viewer's origin | `safehtml.Document` (`convert/convert.go:270`), nonce CSP (`viewer/csp.go`), `lockdownCSP` sandbox on raw files (`viewer/server.go`) | allow-list hole; a fragment carrying script past `safehtml.Rendered`; string rewriting after the sanitiser | holds (tested): headless Chrome on `v1-battery.md` (`<script>`, `onerror`, `<svg onload>`, meta refresh, `<iframe srcdoc>`, `ontoggle`, `<form>`, `javascript:` and `data:` links clicked, `<object>`, `<link rel=prefetch>`, KaTeX `\href{javascript:}` and `\includegraphics`, Mermaid `click … javascript:` and `call`): no dialog, no console error but one 404, and the only request off the viewer was the plain `<img>` |
| V2 | A document in the viewer reads no file outside **its own** opened folder | nothing: confinement is to the union of every granted folder (`servedRoots`, `viewer/roots.go`) | any earlier grant is readable by every later document | **violated (reproduced, D5)** |
| V2a | A document in the viewer reads no file outside the served roots | `confine` + `os.Root` (`viewer/roots.go`) | symlink race; a route that skips `openServed` | holds (argument; covered by `roots_test.go`) |
| V3a | Opening one document serves at most its git work tree or its own folder, never a whole volume or the home folder | `rootFor` (`viewer/roots.go`): stops below home, and a document *directly* in home or `/` grants only itself | a document one level below a filesystem root: its folder is `/Users`, `/Volumes`, `/private/tmp`, which is neither home nor `/` | **violated (reproduced, D7)** |
| V3 | Nothing a document contains can add a served root | `openedByUser` (`viewer/roots.go:68`) | any non-browser client that fetches a URL the document names sends no fetch metadata and is read as the user | **violated (reproduced, D3 = 🎯T67)** |
| V4 | A viewed document makes no network request except loading images (and fonts/stylesheets over https) | CSP `connect-src`, `form-action 'none'`, no script | loads any `http(s)` image on render, with no click; a GET to any LAN or loopback service | by design (README badges) |
| V5 | A document (or its diagram) cannot restyle or replace the viewer's chrome | `cleanStyle`, reserved `vellum-` class prefix (`internal/safehtml`), `<style>` dropped from documents; Mermaid scopes its CSS under the diagram's id | `id` is not reserved, and `getElementById` returns the document's element for chrome nodes that come after the article | **violated (D6)**; Mermaid `themeCSS` stays scoped (tested, see 5) |
| V6 | The viewer's PDF of a document contains no file outside the served roots, and its engine makes no request | `InlineResources`, `WriteSelfContainedPDF`, Mermaid confine hook | tested by 🎯T66/🎯T68 | holds (tested by their suites; not re-attacked) |
| V8 | Assembling the page after the sanitiser keeps the sanitised structure: no document text can end the chrome's markup early or swallow it | regexes in `viewer/links.go`, `viewer/lazy.go`, `viewer/chrome.go` that only see tags and quoted attributes | comment text is raw and the regexes match inside it | **violated (reproduced, D8)** |
| V7 | The viewer's own server-side fetches (PDF images) cannot grant a folder | `fetchRemoteImage` sends `cross-site`/`image` fetch metadata (`viewer/fetch.go`) | a fetch without metadata | holds (argument) |
| C1 | A CLI or MCP conversion embeds no file from outside the document's folder unless the user names it | nothing | WeasyPrint draws `file:` and absolute-path images and embeds `rel=attachment` targets; Prince draws them; pandoc embeds any file named as an image in pptx | **violated (reproduced, D1, D2)** |
| C2 | A CLI or MCP conversion makes no network request the document names unless asked | nothing | engines and pandoc fetch `http(s)` images and stylesheets, without fetch metadata | by design; violated as phrased (D1 reproduction log) |
| C3 | A CLI or MCP conversion cannot get a running viewer to serve a folder | nothing (🎯T67) | an engine GET of `http://127.0.0.1:<viewer>/<folder>/x.md` | **violated (reproduced, D3)** |
| C4 | Mermaid rendered for CLI/MCP reads no image outside the document's folder and makes no request | confine hook in `renderMermaid`/`mmdcCommand` (`convert/mermaid.go`), shared by every sink | a sink that runs mmdc another way | holds (tested: absolute-path image shape, HTML-label `<img>` by path and by http: 0 canary pixels, 0 requests) |
| C5 | KaTeX renders no link or image a formula names | `katex.renderToString` without `trust` (`convert/katex.go:55`) | `trust: true` | holds (argument) |
| C6 | A CLI or MCP conversion runs no script the document carries | engines run none: WeasyPrint and Prince do not execute JavaScript by default; pandoc none | Prince `--javascript` | holds (argument) |
| C7 | The HTML a CLI/MCP conversion writes is inert | nothing: `Sanitize` is off outside the viewer | opening the output HTML in a browser runs the document's script from a `file:` origin | by design |
| I1 | Importing rich text reads no local file and fetches nothing the content names | nothing | pandoc `--extract-media` copies every `<img src>` it can read or download into the media directory | **violated (reproduced, D4)** |
| I2 | Import writes only inside the media directory | pandoc names extracted media by content hash | a crafted media name | holds (observed: SHA-1 names) |

## 3. Confirmed defects

Titles state the desired state. By severity: D1, D2, D7, D3, D5, D4, D6, D8 (numbered in the order found).

### D1. A CLI or MCP PDF carries no file the document names from outside its folder — high

**Severity.** High. A PDF is the artefact people send. Converting a
README from a cloned repo, a document a model wrote, or Markdown an agent
received (MCP `from: content`) silently attaches any file the user can
read; PDF viewers show attachments only in a side panel. Prince does not
attach, but both engines draw outside images.

**Mechanism.** CLI and MCP render with `Sanitize` unset
(`convert/run.go` `writeFileOutput` → `renderPDFHTML`), so goldmark's
`html.WithUnsafe` (`convert/convert.go`) passes raw HTML through.
`WritePDF` (`convert/convert.go:166`) runs WeasyPrint with
`--base-url=file://<doc dir>/` and no `--allowed-protocols`; WeasyPrint
embeds the target of `<a rel="attachment" href>` and
`<link rel="attachment" href>` as a PDF embedded file, and loads `file:`
and absolute-path images. Relative names climb with `../`.

**Reproduction** (`a-attach.md` in the driver; WeasyPrint 70.0, Prince 17):

```markdown
<a rel="attachment" href="file:///…/secret/secret.txt">notes</a>
<a rel="attachment" href="/…/secret/secret.txt">notes2</a>
<link rel="attachment" href="../secret/secret.txt">
![logo](/…/secret/canary.png)
<img src="file:///…/secret/canary.png">
![badge](http://127.0.0.1:<recorder>/badge-from-md.png)
<style>@import url("http://127.0.0.1:<recorder>/import.css");</style>
```

`vellum convert --from file --to file a-attach.md -o a.pdf`:
`pdfdetach -list` shows `3 embedded files … secret.txt`, and
`pdfdetach -save 1` returns `SECRET-TOKEN-1234 private key material`;
`pdfimages -list` shows both 40×40 canaries drawn; the recorder logged
`GET /import.css UA=WeasyPrint 70.0` and `GET /badge-from-md.png`. The
same document through the tree daemon's MCP `convert`
(`from: {media: content}`, `to: {media: file, path: …/out/mcp-a.pdf}`)
gives the same three attachments and both canaries; `../secret/` then
resolves against the output folder. With `--backend prince`: 0 embedded
files, both canaries drawn, both requests made.

**Smallest fix.** Treat document HTML on the CLI/MCP path as the viewer
does: run `safehtml.Document` by default (it already drops `<link>`,
`<style>`, and `rel`), with an explicit opt-in (`--trusted-html`, an MCP
`trusted` flag) for authors who need raw HTML. That closes attachments
and `<style>` imports. Outside images need the second half: pass the
engine an allow-list (WeasyPrint has no path allow-list on its command
line, so this means the viewer's approach, `InlineResources` with a
reader confined to the document's folder, then `WriteSelfContainedPDF`).
Cost: the inliner exists; the reader is a few lines (an `os.Root` on the
document's folder); remote images need a policy decision (fetch, as the
viewer does, or refuse unless asked).

### D2. A pptx (or RTF) conversion embeds only images, and only from the document's folder — high

**Severity.** High for pptx: the deck carries the file byte for byte and
nothing on the slide shows it (a broken picture). RTF refuses non-images
but still embeds outside images.

**Mechanism.** `writePPTX` (`convert/pptx.go:115`) hands the Markdown to
pandoc with `--resource-path=<doc dir>` and no `--sandbox`; pandoc's pptx
writer copies whatever an image reference names into `ppt/media/`, with
no check that it is an image. `HTMLToRTF` (`internal/pandoc/pandoc.go:48`)
likewise runs unsandboxed with `--resource-path`.

**Reproduction** (`d-deck.md`):

```markdown
![logo](/…/secret/canary.png)
![key](/…/secret/secret.txt)
![remote](http://127.0.0.1:<recorder>/pptx-img.png)
```

`vellum convert --from file --to file d-deck.md -o d.pptx`:
`unzip -l` lists `ppt/media/image2.txt` (39 bytes) whose bytes are
`SECRET-TOKEN-1234 private key material`; the recorder logged
`GET /pptx-img.png UA=pandoc/3.14`. To `d.rtf`: two `\pngblip` pictures
(the canary from outside the folder), the text file refused ("image is not
a jpeg or png"), the remote request made.

**Smallest fix.** Inline what the document may use (images inside its
folder) as `data:` URIs before pandoc, as `inlineMermaidPNG` already does
for diagrams, and run pandoc with `--sandbox` (the clipboard route already
does: `HTMLToRTFSandboxed`). `--sandbox` alone would also refuse images
inside the folder, which breaks today's decks; hence the inlining step.

### D7. Opening one document never serves a whole volume or the home folder — high (with D3), medium alone

**Severity.** High in combination with D3: one converted document gets all
of `/Users` (the home folder included) served to every page and to any
client of the port, persistently. Medium alone: a user who opens a
document saved at `/Volumes/<drive>/notes.md` or `/tmp/x.md` serves the
whole drive or `/private/tmp` to every later document (D5).

**Mechanism.** `rootFor` (`viewer/roots.go`) returns the document's folder
unless a `.git` is found below the home folder. Its guard covers two cases:
the folder *is* home, or the folder *is* a filesystem root. A folder one
level down (`/Users`, `/Volumes`, `/Volumes/Storage`, `/private/tmp`) is
neither, so the walk finds no `.git`, stops at `/`, and grants the folder.
The grant does not need the document to exist (F6).

**Reproduction** (tree viewer, fresh cache, navigation headers):
`GET /Users/notes.md` (absent) → 404, and `roots.json` becomes
`[{"path": "/Users"}]`; a `HEAD` of this worktree's `CLAUDE.md` under
`/Users/marcelo/…` goes from 403 to 200. Likewise `/Volumes/Storage/notes.md`
→ `/Volumes/Storage` (a file in the scratch tree went from 403 to 200) and
`/tmp/notes.md` → `/private/tmp`. Through D3 (`repro.py`, case D7): a
document converted by the CLI whose image is
`http://127.0.0.1:P/Volumes/vellum-probe.md` makes `roots.json`
`["/Volumes"]`, and a canary deep under it goes from 403 to 200.

**Smallest fix.** In `rootFor`, refuse (or grant the file only) when the
folder's parent is a filesystem root, and when the folder contains the
home folder; and grant only documents that exist (F6).

### D3. Converting a document from the CLI or MCP cannot get the viewer to serve a folder — medium (🎯T67, reconfirmed)

**Severity.** Medium. A grant persists in `roots.json` and opens the
folder to every later page and request (D5), but reading it still needs a
client that can reach the port.

**Mechanism.** `openedByUser` (`viewer/roots.go:68`) treats a request with
no `Sec-Fetch-Site` as the user. WeasyPrint, Prince and pandoc send none.

**Reproduction** (`b-grant.md`, tree viewer on port P, fresh cache):
`GET http://127.0.0.1:P/…/secret/secret.txt` → 403. Convert

```markdown
![x](http://127.0.0.1:P/…/secret/x.md)
```

with `vellum convert … -o b.pdf` (`x.md` does not exist). `roots.json` now
lists `…/secret`, and the same GET returns 200 with the secret.

**Smallest fix.** The owner's grant-rule decision recorded in 🎯T66's note;
of its options, the capability token for tools plus "browser top-level
navigation only" closes this without touching converters.

### D4. Importing rich text copies no local file and fetches nothing it names — medium

**Severity.** Medium. HTML on the clipboard comes from web pages the user
copied; MCP `from: content, format: html` comes from whatever an agent
holds. The copy lands in the import cache (0644) and its path is
returned in `assets`; the Markdown then names the copy, so any later
conversion of it (pptx, D2) embeds it with no outside path left to notice.

**Mechanism.** `ImportFile`/`ImportBytes` (`importer/importer.go`) run
`pandoc --extract-media=<dir>` with no `--sandbox`; pandoc reads every
image reference it can (absolute paths, `file:` URLs) or downloads it, and
writes the bytes into the media directory.

**Reproduction** (`c-import.html`):
`vellum convert --from file --to content c-import.html` prints Markdown
naming `…/import/<hash>/6e238b39….txt`; that file holds
`SECRET-TOKEN-1234 private key material` (from `<img src="/…/secret.txt">`)
and `…png` is the canary (from `file:`); the recorder logged
`GET /import-img.png UA=pandoc/3.14` and two `GET /iframe.html`
(pandoc fetches iframe sources). A `file:` iframe of a local HTML file was
not inlined (tested; negative).

Two follow-ups, both confirmed: (1) with a loopback server answering
`frame.html` with `<p>REMOTE-IFRAME-TEXT-9</p>`, the imported Markdown
contains `REMOTE-IFRAME-TEXT-9` inside `<div class="iframe">`: import
splices fetched remote content into the result, not only images. (2) A
source file at mode 0600 is copied at 0644, in cache folders created 0755
(`importer.EnsureMediaDir`); on a default macOS install `~/Library` is
0700, so other accounts cannot reach it, but the mode is not preserved.

**Smallest fix.** Add `--sandbox` to the import pandoc run. Embedded media
in docx/odt/epub still extract (they are inside the input); references to
outside files and URLs are dropped with a warning, which becomes a soft
error.

### D5. A document in the viewer reads only from its own folder, not from every folder ever opened — medium

**Severity.** Medium. There is no script, so the page cannot send what it
reads; but the viewer's PDF and clipboard routes carry it out (inlined into
the PDF the user downloads, or named on the clipboard for a paste target
to fetch).

**Mechanism.** `servedRoots` (`viewer/roots.go`) is one global list; every
route's `confine` accepts any root. The 🎯T63 rule "a page cannot open a
folder" holds; "a page reads only its own folder" was never the rule.

**Reproduction** (tree viewer, fresh cache): open `…/private/notes.md`
(navigation, `Sec-Fetch-Site: none`), then `…/hostile/README.md`
containing `![x](/…/private/photo.png)`. `roots.json` lists both folders.
`GET /_vellum/pdf?path=…/hostile/README.md` (same-origin) returns a PDF
whose `pdfimages -list` shows the 40×40 photo from `private/`; the page's
own image request for it returns 200.

**Smallest fix.** Confine per document: the routes that render a document
(`/<doc>.md`, `/_vellum/pdf`, `/_vellum/clipboard`, `/_vellum/fragment`)
already know its path; resolve its root with `rootFor` and pass only that
root to `InlineResources` and the image check. For plain file requests from
a page, `Referer` (sent same-origin by default) names the document; a
request whose referring document's root does not contain the file is
refused. Cost: moderate; the routes take one more argument, tests per route.

### D6. A document cannot take over the viewer's dialogs by reusing their ids — low

**Severity.** Low. Self-contained confusion and denial of chrome features
(task consent, editor, lightbox); nothing crosses a file or network
boundary.

**Mechanism.** `safehtml` reserves the `vellum-` prefix for `class` but not
`id` (`globalAttrs` allows any `id`; `cleanClass` only filters classes).
`chromeClose` (`viewer/chrome.go`) emits `#vellum-consent`, `#vellum-gone`,
`#vellum-editor*` and `#vellum-lightbox*` **after** the article, and
`chrome.js` looks them up with `document.getElementById`, which returns the
first in tree order: the document's.

**Reproduction** (`v1-battery.md`, tree viewer, `browse.cjs`). The document
holds `<div id="vellum-consent">FAKE CONSENT: enter your password</div>`,
`<div id="vellum-lightbox-content">…</div>` and one task `- [ ] a task`.
After load, `document.getElementById("vellum-consent")` is the document's
div (`consentIsChrome: false`, text `FAKE CONSENT…`), likewise the lightbox
slot. Clicking the task box: the real consent dialog stays `hidden: true`,
the document's div is what `showConsent` "shows", and no
`/_vellum/task-toggle` request is made; the file is unchanged. With
`id="vellum-editor-host"` the editor would mount inside the document's
element (read, not run).

**Smallest fix.** In `cleanAttrs`, drop (or prefix with `user-content-`, as
GitHub does) any `id` and `name` that start with `vellum-`; or move the
chrome lookups to `chrome.querySelector` scoped to the chrome's own
containers.

### D8. Nothing a document writes stops the viewer's own script or hides its later text — low

**Severity.** Low. A document disables the viewer's chrome for itself
(TOC, diagrams and math never load, the editor and consent dialogs are
gone) and can hide the rest of its own text from the reader while the file,
the PDF route and an editor elsewhere still hold it. No file or network
boundary is crossed; no script of the document's runs.

**Mechanism.** `safehtml.Document` keeps every comment verbatim
(`cleanDocumentNode`: "Comments carry vellum's own placeholders"). The
steps after it are regular expressions over the serialised page that do
not know where comments are: `lazyLoadImages` (`viewer/lazy.go:51`)
rewrites `<img a-->` inside a comment into `<img a-- loading="lazy"
decoding="async">`, removing the comment's `-->`, so the browser's comment
runs to the end of the page; `injectChrome` (`viewer/chrome.go:65`) takes
the *first* `</body>` (`bodyCloseRe`) as the body's end, which can be one
inside a document comment, and puts the chrome's closing markup and its
nonce'd script there, inside the comment. `rewriteFragmentHrefs`
(`<!--<a href=#x-->`) breaks a comment the same way.

**Reproduction** (`h-control.md`, `h-comment.md`, `h-body.md`; tree
viewer; `probe2.cjs` in headless Chrome). Control (an ordinary comment):
text after it visible, 2 TOC links, 1 diagram, 2 nonce'd scripts, editor
dialog present. With `<!--<img a-->`: text after it gone, 0 TOC links,
0 diagrams, 1 script (the head's theme script only), no editor dialog;
the served page holds `<!--<img a-- loading="lazy" decoding="async">`.
With `<!-- </body> -->`: text visible, but 0 TOC links, 0 diagrams,
1 script, no editor dialog.

**Smallest fix.** In `cleanDocumentNode`, keep a comment only when it is
one of vellum's own placeholders (exact grammar, the ids `ResolveFragments`
knows) and drop the rest; that alone closes both spellings, since the
later regexes then never see document-controlled comment text. The
structural fix is F7.

## 4. Design findings

### F1. Two trust postures, and the dangerous one is the default for agents

The viewer treats a document as hostile (allow-list sanitiser, nonce CSP,
served roots, fetch-free engines, confined Mermaid); the CLI and MCP treat
it as the user's own (raw HTML, engines with full file and network reach,
pandoc unsandboxed). Nothing in the MCP tool description or `--help` says
so, and MCP is exactly where documents arrive that the user did not write
(an agent converting what it fetched or generated). D1, D2, D3 and D4 are
all this one decision. Change: one "untrusted render" pipeline used by
every sink by default — `Sanitize`, `InlineResources` with a reader
confined to the document's folder (an `os.Root`), `WriteSelfContainedPDF`,
pandoc `--sandbox` with images pre-inlined — and a single explicit opt-in
(`--trusted`, MCP `trusted: true`) that restores today's behaviour. Cost:
the viewer already has every piece; the work is threading a root through
`convert.Run` and a decision on remote images for CLI (fetch with the
viewer's fetcher, or only when asked). Medium: a day plus tests per sink.

### F2. Who may grant a folder, and how much, are both decided by accident

The grant rule conflates "no fetch metadata" with "the user" (D3, the
🎯T66 note), the grant's width comes from `rootFor`'s two special cases
(home and `/`) rather than from a rule (D7), and a grant, once made, is
global and persistent for every document (D5). Each is defensible alone;
together one hostile request widens every later page's reach to a whole
volume. Change: grant by capability (a per-install secret presented by
`vellum view` and the Finder handler, the 🎯T66 note's option 2) plus
browser top-level navigation; never grant a folder whose parent is a
filesystem root or that contains the home folder (`rel(dir, home)` not
starting with `..` and dir ≠ home is the rule); and confine each document
route to the document's own root (D5's fix). Cost: medium; `vellum view`,
`scripts/verify-release.sh` and the roots tests change.

### F3. The sanitiser reserves a namespace for classes only

`reservedClassPrefix` exists so a document cannot dress up as chrome, but
ids (and `name`) share the page's namespace with chrome lookups (D6).
Change: apply the same prefix rule to `id` and `name`, and add the
`user-content-` prefix that GitHub uses so heading anchors keep working.
Cost: small, but anchor rewriting (`rewriteFragmentHrefs`, TOC) must agree.

### F4. Every Mermaid diagram on a page has the same id

mmdc emits `<svg id="my-svg">` and scopes its CSS as `#my-svg …`, so each
diagram's stylesheet (including any `themeCSS` and `classDef`) applies to
every diagram on the page, and to a document heading whose text slugs to
"my-svg". Observed in the fragment for `e-style.md` (a scoped
`#my-svg #my-svg{position:fixed…}` rule). Not a boundary crossing: the
scoping held, so a diagram cannot reach the chrome. Change: pass
`--svgId vellum-diagram-<fragment id>` to mmdc. Cost: small; fragment ids
are already content hashes.

### F5. No test can fail for the CLI and MCP boundaries

Every confinement test (`viewer/pdf_*_test.go`, `roots_test.go`,
`clipboard_confine_test.go`) exercises the viewer. Nothing asserts what a
CLI or MCP conversion may read or fetch, so D1, D2 and D4 are not
regressions anyone could see; the 🎯T66 attestation lists the CLI path as
residue, which is accurate but not enforced. Change: once F1 decides the
rule, turn `repro.py`'s cases into Go tests under `convert/` and `mcp/`
(they need only weasyprint, pandoc and poppler, already gate
dependencies), each failing today.

### F6. A grant happens for documents that do not exist

`handlePath` grants before it knows whether the `.md` exists
(`viewer/server.go`, "a grant that fails (no such folder) leaves the check
below to refuse the path"): only the *folder* must exist. So any probe for
a made-up name grants a real folder (D3, D7 both used absent names). The
waiting page for a missing document is a feature, but it does not need a
grant to show. Change: grant only when the document exists, or let the
waiting page watch without granting. Cost: small.

### F7. The page is assembled by regular expressions after it is sanitised

`serveMarkdown` runs six string rewrites over the sanitised page
(`rewriteMarkdownHrefs`, `rewriteFragmentHrefs`, `ensureBaseHref`,
`reserveDiagramBoxes`, `lazyLoadImages`, `injectChrome`), each with its own
regex for a tag or attribute. They are safe against attribute values
(`html.Render` escapes quotes and angle brackets there, and the inserted
paths are percent-escaped), but not against comment text, which the
sanitiser passes through raw (D8). Every new rewrite inherits the risk.
Change: do these edits on the parsed tree inside `safehtml` or one
post-pass over `x/net/html` nodes, serialising once at the end; the
chrome then wraps the body node rather than splicing at the first
`</body>`. Cost: medium; the rewrites are small, the tests in
`viewer/links_test.go`, `lazy_test.go` and `chrome_test.go` would move
with them.

## 5. Unconfirmed suspicions

- **Clipboard HTML flavour grants through the paste target.** The viewer's
  clipboard copy carries the document's image URLs; a document image named
  `http://127.0.0.1:18742/<folder>/x.md` is refused in the browser (fetch
  metadata `same-origin`) but a paste target that fetches it without fetch
  metadata (Mail, Notes, a chat client) would grant the folder (D3 by
  another route). Not run: it writes the user's real clipboard. 🎯T66
  residue (a) records the same thing in general terms.
- **The tunnel.** 🎯T66's note mentions a tunnel presenting a loopback
  `Host`. If one exists, D7 means one request through it serves the whole
  of `/Users`. No tunnel configuration is in this repository; not checked.
- **Prince attachments.** Prince did not honour `rel=attachment`; Prince's
  own attachment syntax (`prince-pdf-attachment` / `--attach`) was not
  tried from document CSS. Since D1's fix sanitises `<style>`, it would be
  covered either way.

## 6. Coverage

Read in full: `viewer/server.go`, `viewer/roots.go`, `viewer/csp.go`,
`viewer/fetch.go`, `viewer/links.go`, `internal/safehtml/safehtml.go`, `mcp/server.go`, the
two PDF backend files.
Read in part: `convert/convert.go` 120–300, `convert/run.go` 262–510,
`convert/pptx.go` 100–180, `importer/importer.go` 40–200,
`viewer/actions.go` 24–120, `viewer/lazy.go` 30–80 and 245–366, `viewer/chrome.go`
60–241, `viewer/chrome.js` (element lookups, consent, fragment loader),
`viewer/edit.go` (method and extension checks), `convert/katex.go` (render
options), `convert/mermaid.go` (confinement entry points). Bullseye 🎯T57,
🎯T66–🎯T69 read; the vellum rows of `spare-capacity.md` and
`defect-hunts-2026-11-20.md` read.

Run: CLI PDF (WeasyPrint and Prince), RTF, pptx, HTML import; MCP over
the tree daemon's `/mcp`; the viewer page, fragment and PDF routes; a
headless-Chrome battery. Not run: the clipboard (the user's real
pasteboard), `vellum view` and the Finder handler (they open the user's
browser), Windows (🎯T69), `vellum --mcp` over stdio (same `convert.Run`
as HTTP, by reading).

Not reached: `viewer/task.go` and `viewer/watch*.go` beyond their path
checks, `adf/`, `importer/pdf.go` (Poppler: `pdftoppm`/`pdftotext` read
only the given PDF), `clipboard/` beyond the 🎯T66 notes, `config/`
(a hostile config file is outside this question), the editor bundle.
