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
// Inside a document's <svg> the drawing's own elements, its <style> and
// its style attributes are expected; what can run is not.
func activeContent(t *testing.T, out string) []string {
	t.Helper()
	nodes, err := html.ParseFragment(strings.NewReader(out), bodyContext)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	var walk func(n *html.Node, inSVG bool)
	walk = func(n *html.Node, inSVG bool) {
		if n.Type == html.ElementNode {
			name := strings.ToLower(n.Data)
			if n.Namespace == svgNamespace && name == svgRoot {
				inSVG = true
			}
			switch name {
			case "script", "iframe", "frame", "object", "embed", "applet", "meta", "base", "link",
				"math", "form", "template", "noscript", "textarea", "button", "select",
				"handler", "listener", "discard":
				found = append(found, "<"+n.Data+">")
			case "style", "svg", "foreignobject":
				if !inSVG || (name == "style" && n.Namespace == "") {
					found = append(found, "<"+n.Data+">")
				}
			case "input":
				if inSVG {
					found = append(found, "<input> in a drawing")
				}
			case "animate", "set", "animatemotion", "animatetransform", "animatecolor":
				if animatesLink(n) {
					found = append(found, "<"+n.Data+"> of a link")
				}
			}
			if n.Namespace != "" && !(inSVG && n.Namespace == svgNamespace) {
				found = append(found, "namespace "+n.Namespace)
			}
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				switch {
				case strings.HasPrefix(key, "on"):
					found = append(found, key+"=")
				case key == "style" && inSVG:
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
			walk(c, inSVG)
		}
	}
	for _, n := range nodes {
		walk(n, false)
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
		{"svg foreignObject script", `<svg><foreignObject><div><script>alert(1)</script></div></foreignObject></svg>`},
		{"svg foreignObject iframe", `<svg><foreignObject><iframe src="https://example.com"></iframe></foreignObject></svg>`},
		{"svg foreignObject form", `<svg><foreignObject><form action="https://evil.example/"><input name=q><button>go</button></form></foreignObject></svg>`},
		{"svg foreignObject checkbox", `<svg><foreignObject><input type=checkbox data-task-index=0></foreignObject></svg>`},
		{"svg foreignObject html style", `<svg><foreignObject><style>body{display:none}</style></foreignObject></svg>`},
		{"svg foreignObject math", `<svg><foreignObject><math><mi xlink:href="javascript:alert(1)">x</mi></math></foreignObject></svg>`},
		{"svg link javascript", `<svg><a href="javascript:alert(1)"><text>x</text></a></svg>`},
		{"svg xlink javascript", `<svg><a xlink:href="  java&#9;script:alert(1)"><text>x</text></a></svg>`},
		{"svg set href", `<svg><a href="#x"><set attributeName="href" to="javascript:alert(1)"/><text>x</text></a></svg>`},
		{"svg animate xlink href", `<svg><a><animate attributeName=" xlink:href " values="javascript:alert(1)"/></a></svg>`},
		{"svg use data document", `<svg><use href="data:image/svg+xml,&lt;svg id='x' xmlns='http://www.w3.org/2000/svg'&gt;&lt;script&gt;alert(1)&lt;/script&gt;&lt;/svg&gt;#x"/><a href="data:text/html,<script>alert(1)</script>">x</a></svg>`},
		{"svg handler elements", `<svg><handler>alert(1)</handler><listener event="click"/><discard/></svg>`},
		{"svg event attributes", `<svg onclick=alert(1)><rect onmouseover=alert(1) onbegin=alert(1) width=5 height=5 /></svg>`},
		{"svg reserved class", `<svg class="vellum-toolbar"><g class="vellum-deferred-mermaid x"></g></svg>`},
		{"svg link target", `<svg><a href="https://example.com" target="_top"><text>x</text></a></svg>`},
		{"svg title breakout", `<svg><title><style><img src=x onerror=alert(1)></style></title></svg>`},
		{"svg style breakout", `<svg></p><style><a title="</style><img src onerror=alert(1)>">`},
		{"svg desc table", `<svg><desc><table><tr><td><script>alert(1)</script></td></tr></table></desc></svg>`},
		{"svg comment breakout", `<svg><!--><script>alert(1)</script>--></svg>`},
		{"svg element with no root", `<circle r=5 onclick=alert(1)></circle><foreignObject><script>alert(1)</script></foreignObject>`},
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

// A drawing a document writes inline stays a drawing: real SVG, with its
// text, links, styles, gradients, animation and labels.
func TestDocument_KeepsInlineSVG(t *testing.T) {
	const drawing = `<svg viewBox="0 0 100 40" width="200" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" onload="x()">` +
		`<style>.a > text { fill: red } </style>` +
		`<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/></linearGradient><clipPath id="c"><rect width="10" height="10"/></clipPath></defs>` +
		`<rect class="a vellum-toolbar" width="100" height="40" fill="url(#g)" style="stroke:blue" clip-path="url(#c)"/>` +
		`<a xlink:href="https://example.com/p" target="_blank"><text x="5" y="20" font-size="12">Hi &amp; <tspan font-weight="bold">bye</tspan></text></a>` +
		`<a href="other.md"><text>doc</text></a>` +
		`<use href="#g"/><image href="pic.png" width="5" height="5"/>` +
		`<circle r="3"><animate attributeName="r" from="1" to="3" dur="1s" repeatCount="indefinite"/></circle>` +
		`<g><animateTransform attributeName="transform" type="rotate" from="0" to="360" dur="2s"/></g>` +
		`<foreignObject x="0" y="0" width="100" height="40"><div xmlns="http://www.w3.org/1999/xhtml" style="display:table-cell;white-space:nowrap"><span class="label" onclick="x()">A<br>B</span></div></foreignObject>` +
		`<script>alert(1)</script><!-- note -->` +
		`</svg>`
	const want = `<svg viewBox="0 0 100 40" width="200" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">` +
		`<style>.a &gt; text { fill: red } </style>` +
		`<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"></stop></linearGradient><clipPath id="c"><rect width="10" height="10"></rect></clipPath></defs>` +
		`<rect class="a" width="100" height="40" fill="url(#g)" style="stroke:blue" clip-path="url(#c)"></rect>` +
		`<a xlink:href="https://example.com/p"><text x="5" y="20" font-size="12">Hi &amp; <tspan font-weight="bold">bye</tspan></text></a>` +
		`<a href="other.md"><text>doc</text></a>` +
		`<use href="#g"></use><image href="pic.png" width="5" height="5"></image>` +
		`<circle r="3"><animate attributeName="r" from="1" to="3" dur="1s" repeatCount="indefinite"></animate></circle>` +
		`<g><animateTransform attributeName="transform" type="rotate" from="0" to="360" dur="2s"></animateTransform></g>` +
		`<foreignObject x="0" y="0" width="100" height="40"><div xmlns="http://www.w3.org/1999/xhtml" style="display:table-cell;white-space:nowrap"><span class="label">A<br/>B</span></div></foreignObject>` +
		`</svg>`
	if got := Document(drawing); got != want {
		t.Fatalf("a block drawing changed\n got %s\nwant %s", got, want)
	}
	if got := Document(want); got != want {
		t.Fatalf("a cleaned drawing changed when cleaned again\n got %s\nwant %s", got, want)
	}
	inSentence := `<p>before <svg width="10" height="10"><circle r="5"/></svg> after</p>`
	if got, want := Document(inSentence), `<p>before <svg width="10" height="10"><circle r="5"></circle></svg> after</p>`; got != want {
		t.Fatalf("a drawing in a sentence changed\n got %s\nwant %s", got, want)
	}
	// Math markup written by hand is still not allowed; vellum renders
	// math itself.
	if got := Document(`<math><mi>x</mi></math>`); got != "" {
		t.Fatalf("MathML passed: %s", got)
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

// FuzzDocument holds the sanitiser to its promise on arbitrary markup:
// nothing that can run comes out. activeContent parses the output the way
// a browser does, so markup that only turns active on a second parse is
// caught too.
func FuzzDocument(f *testing.F) {
	for _, seed := range []string{
		`<svg><style>.a{fill:red}</style><a xlink:href="x"><text>t</text></a><foreignObject><div style="x"><p>l</p></div></foreignObject></svg>`,
		`<svg><title><style><img src=x onerror=alert(1)></style></title></svg>`,
		`<svg></p><style><a title="</style><img src onerror=alert(1)>">`,
		`<svg><desc><table><tr><td><script>alert(1)</script></td></tr></table></desc></svg>`,
		`<svg><foreignObject><math><mtext><mglyph><style><img src onerror=alert(1)></style></mglyph></mtext></math></foreignObject></svg>`,
		`<p>a <svg><set attributeName="href" to="javascript:alert(1)"/></svg> b</p><math><mi>x</mi></math>`,
		`<svg><a href="javascript:alert(1)"><animate attributeName=href /></a><script>alert(1)</script></svg>`,
		`<table><svg><tr><td><svg><p><style><!--</style><img src onerror=alert(1)>--></style>`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Document(in)
		if found := activeContent(t, out); len(found) > 0 {
			t.Fatalf("active content survived: %v\nin:  %q\nout: %q", found, in, out)
		}
		if found := activeContent(t, Document(out)); len(found) > 0 {
			t.Fatalf("active content after a second pass: %v\nin: %q", found, in)
		}
	})
}
