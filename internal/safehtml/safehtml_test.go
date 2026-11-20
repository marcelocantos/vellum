// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package safehtml

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// activeContent lists what a browser would run, load as a separate
// document, or use to redirect the page's URLs, found by parsing out.
func activeContent(t *testing.T, out string) []string {
	t.Helper()
	nodes, err := html.ParseFragment(strings.NewReader(out), bodyContext)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "script", "iframe", "frame", "object", "embed", "applet", "meta", "base", "link",
				"style", "svg", "math", "form", "template", "noscript", "textarea", "foreignobject":
				found = append(found, "<"+n.Data+">")
			}
			if n.Namespace != "" {
				found = append(found, "namespace "+n.Namespace)
			}
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				switch {
				case strings.HasPrefix(key, "on"):
					found = append(found, key+"=")
				case key == "href" || key == "src" || key == "cite" || key == "action" ||
					key == "formaction" || key == "srcset" || key == "background" || key == "data":
					v := strings.ToLower(strings.Join(strings.FieldsFunc(a.Val, func(r rune) bool { return r <= ' ' }), ""))
					if strings.HasPrefix(v, "javascript:") || strings.HasPrefix(v, "vbscript:") ||
						(strings.HasPrefix(v, "data:") && !strings.HasPrefix(v, "data:image/")) {
						found = append(found, key+"="+a.Val)
					}
					if key == "srcset" || key == "action" || key == "formaction" || key == "background" || key == "data" {
						found = append(found, key+"=")
					}
				case key == "style" && a.Val != "" && !strings.HasPrefix(a.Val, "text-align:"):
					found = append(found, "style="+a.Val)
				case key == "class" && strings.Contains(a.Val, reservedClassPrefix):
					found = append(found, "class="+a.Val)
				case key == "name" && n.Data != "a":
					found = append(found, "name= on "+n.Data)
				case key == "http-equiv" || key == "srcdoc" || key == "target" || key == "formtarget":
					found = append(found, key+"=")
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return found
}

func TestDocument_RemovesActiveContent(t *testing.T) {
	payloads := []struct{ name, in string }{
		{"script element", `<script>alert(1)</script>`},
		{"script src", `<script src="evil.js"></script>`},
		{"script in table", `<table><tr><td><script>alert(1)</script></td></tr></table>`},
		{"script after nested quote", `<p title="</p><script>alert(1)</script>">x</p>`},
		{"img onerror", `<img src=x onerror=alert(1)>`},
		{"onerror upper case", `<IMG SRC=x ONERROR="alert(1)">`},
		{"body onload", `<body onload=alert(1)>`},
		{"div onmouseover", `<div onmouseover="alert(1)">hover</div>`},
		{"details ontoggle", `<details open ontoggle=alert(1)><summary>s</summary></details>`},
		{"a javascript", `<a href="javascript:alert(1)">x</a>`},
		{"a javascript mixed case", `<a href="JaVaScRiPt:alert(1)">x</a>`},
		{"a javascript entity", `<a href="&#106;avascript:alert(1)">x</a>`},
		{"a javascript tab", "<a href=\"java\tscript:alert(1)\">x</a>"},
		{"a javascript newline", "<a href=\"java\nscript:alert(1)\">x</a>"},
		{"a javascript leading space", `<a href="  javascript:alert(1)">x</a>`},
		{"a javascript control char", "<a href=\"\x01javascript:alert(1)\">x</a>"},
		{"a vbscript", `<a href="vbscript:msgbox(1)">x</a>`},
		{"a data html", `<a href="data:text/html,<script>alert(1)</script>">x</a>`},
		{"a data html base64", `<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">x</a>`},
		{"img javascript", `<img src="javascript:alert(1)">`},
		{"img data html", `<img src="data:text/html,<script>alert(1)</script>">`},
		{"img srcset", `<img srcset="javascript:alert(1) 1x">`},
		{"svg script", `<svg><script>alert(1)</script></svg>`},
		{"svg onload", `<svg onload=alert(1)></svg>`},
		{"svg animate href", `<svg><a><animate attributeName=href values=javascript:alert(1) /><text y=20>x</text></a></svg>`},
		{"svg foreignObject", `<svg><foreignObject><img src=x onerror=alert(1)></foreignObject></svg>`},
		{"math href", `<math><mtext><a href="javascript:alert(1)">x</a></mtext></math>`},
		{"iframe", `<iframe src="https://example.com"></iframe>`},
		{"iframe srcdoc", `<iframe srcdoc="<script>alert(1)</script>"></iframe>`},
		{"object", `<object data="evil.swf"></object>`},
		{"embed", `<embed src="evil.swf">`},
		{"meta refresh", `<meta http-equiv="refresh" content="0;url=javascript:alert(1)">`},
		{"base", `<base href="https://evil.example/">`},
		{"link stylesheet", `<link rel="stylesheet" href="https://evil.example/x.css">`},
		{"style element", `<style>body{background:url(https://evil.example/)}</style>`},
		{"style attribute overlay", `<div style="position:fixed;top:0;left:0;width:100%;height:100%">x</div>`},
		{"style attribute expression", `<div style="background:url(javascript:alert(1))">x</div>`},
		{"reserved class", `<div class="vellum-toolbar vellum-deferred">x</div>`},
		{"form", `<form action="https://evil.example/"><button formaction="javascript:alert(1)">go</button></form>`},
		{"input image", `<input type="image" src=x onerror=alert(1)>`},
		{"input text", `<input type="text" value="x" onfocus=alert(1) autofocus>`},
		{"textarea", `<textarea><script>alert(1)</script></textarea>`},
		{"noscript", `<noscript><p title="</noscript><img src=x onerror=alert(1)>"></noscript>`},
		{"template", `<template><script>alert(1)</script></template>`},
		{"img name clobbering", `<img name="getElementById" src="x.png">`},
		{"a target", `<a href="https://example.com" target="_blank">x</a>`},
		{"comment breakout", `<!-- --!><img src=x onerror=alert(1)> -->`},
		{"unclosed comment", `<!-- <img src=x onerror=alert(1)>`},
		{"cdata", `<![CDATA[<img src=x onerror=alert(1)>]]>`},
		{"xmp", `<xmp><img src=x onerror=alert(1)></xmp>`},
		{"plaintext", `<plaintext><img src=x onerror=alert(1)>`},
		{"unterminated attribute", `<img src=x alt="` + "\n\n" + `<p onclick=alert(1)>x</p>`},
		{"dangling body close", `</body><script>alert(1)</script>`},
		{"dialog", `<dialog open onclose=alert(1)>x</dialog>`},
		{"video", `<video src=x onerror=alert(1)></video>`},
	}
	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			out := Document(p.in)
			if found := activeContent(t, out); len(found) > 0 {
				t.Fatalf("active content survived: %v\nin:  %s\nout: %s", found, p.in, out)
			}
			if strings.Contains(strings.ToLower(out), "javascript:") && strings.Contains(out, "href=") {
				t.Fatalf("javascript: URL survived in an attribute:\n%s", out)
			}
		})
	}
}

// What a Markdown document legitimately renders must come through.
func TestDocument_KeepsDocumentMarkup(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"heading id and source lines", `<h1 id="head-one" data-source-line="6" data-source-end="6">Head <em>one</em></h1>`,
			`<h1 id="head-one" data-source-line="6" data-source-end="6">Head <em>one</em></h1>`},
		{"table alignment", `<table><thead><tr><th style="text-align:left">a</th></tr></thead></table>`,
			`<th style="text-align:left">a</th>`},
		{"task checkbox", `<li data-task-index="0" data-task-line="3"><input checked="" type="checkbox"> t</li>`,
			`<li data-task-index="0" data-task-line="3"><input checked="" type="checkbox"/> t</li>`},
		{"footnote ref", `<sup id="fnref:1"><a href="#fn:1" class="footnote-ref" role="doc-noteref">1</a></sup>`,
			`<sup id="fnref:1"><a class="footnote-ref" href="#fn:1" role="doc-noteref">1</a></sup>`},
		{"chroma", `<pre class="chroma"><code><span class="line"><span class="kd">func</span></span></code></pre>`,
			`<pre class="chroma"><code><span class="line"><span class="kd">func</span></span></code></pre>`},
		{"links", `<a href="https://a.example/x?y#z">a</a><a href="other.md#s">b</a><a href="mailto:me@example.com">c</a><a href="/abs/p.md">d</a>`,
			`<a href="https://a.example/x?y#z">a</a><a href="other.md#s">b</a><a href="mailto:me@example.com">c</a><a href="/abs/p.md">d</a>`},
		{"images", `<img src="pic.png" alt="alt em" title="title" width="40"><img src="data:image/png;base64,iVBOR" alt="">`,
			`<img src="pic.png" alt="alt em" title="title" width="40"/><img src="data:image/png;base64,iVBOR" alt=""/>`},
		{"details", `<details open><summary>S</summary><p>body</p></details>`, `<details open=""><summary>S</summary><p>body</p></details>`},
		{"placeholder comment", `<p><!--MATH:0--></p>`, `<p><!--MATH:0--></p>`},
		{"unknown element unwrapped", `<center><font color="red">kept text</font></center>`, `kept text`},
		{"entities stay escaped", `<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>`, `<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>`},
		{"kbd sup sub br", `<kbd>⌘</kbd>x<sup>2</sup><sub>i</sub><br>`, `<kbd>⌘</kbd>x<sup>2</sup><sub>i</sub><br/>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := Document(c.in)
			if !strings.Contains(out, c.want) {
				t.Fatalf("lost markup\nin:   %s\nout:  %s\nwant: %s", c.in, out, c.want)
			}
		})
	}
}

func TestRendered_StripsActiveContentKeepsDrawing(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" onload="alert(1)">` +
		`<style>#m .node rect{fill:#eee} a > b {}</style>` +
		`<script>alert(1)</script>` +
		`<g class="node"><rect width="5" height="5"/><foreignObject><div onclick="alert(1)"><span class="nodeLabel">A</span></div></foreignObject></g>` +
		`<a xlink:href="javascript:alert(1)"><text>x</text></a>` +
		`<a href="#frag"><text>y</text></a>` +
		`<animate attributeName="href" to="javascript:alert(1)"/>` +
		`<image href="data:image/png;base64,iVBOR"/>` +
		`</svg>`
	out := Rendered(svg)
	for _, bad := range []string{"<script", "onload", "onclick", "javascript:", "<animate"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(bad)) {
			t.Fatalf("%s survived:\n%s", bad, out)
		}
	}
	for _, good := range []string{`viewBox="0 0 10 10"`, `<foreignObject>`, `class="nodeLabel"`, `a &gt; b`, `href="#frag"`, `href="data:image/png;base64,iVBOR"`} {
		if !strings.Contains(out, good) {
			t.Fatalf("%s lost:\n%s", good, out)
		}
	}
	katex := `<span class="katex"><span class="katex-html" aria-hidden="true"><span class="base"><span class="strut" style="height:0.6644em;"></span><span class="mord mathnormal">x</span></span></span></span>`
	if got := Rendered(katex); got != katex {
		t.Fatalf("KaTeX markup changed:\n got %s\nwant %s", got, katex)
	}
}
