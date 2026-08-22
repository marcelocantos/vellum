// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package adf

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Convert transforms Markdown into a version-1 ADF Document.
// It does not contact Confluence or run mmdc.
func Convert(markdown string, args *ConvertArgs) (*Document, error) {
	if args == nil {
		args = &ConvertArgs{}
	}
	switch args.imagePolicy() {
	case ImagePolicyExternal, ImagePolicyLink, ImagePolicyFail:
	default:
		return nil, fmt.Errorf("adf: unknown ImagePolicy %q", args.ImagePolicy)
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	// KaTeX residue: rewrite $…$ / $$…$$ into ```latex fences before parse.
	source := []byte(extractMathAsLatexFences(markdown))
	root := md.Parser().Parse(text.NewReader(source))

	c := &converter{
		source: source,
		args:   args,
	}
	content, err := c.blocks(root)
	if err != nil {
		return nil, err
	}
	if content == nil {
		content = []Node{}
	}
	return &Document{
		Version: 1,
		Type:    "doc",
		Content: content,
	}, nil
}

// ConvertJSON is Convert plus JSON encoding (no HTML escape; compact).
func ConvertJSON(markdown string, args *ConvertArgs) ([]byte, error) {
	doc, err := Convert(markdown, args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

type converter struct {
	source []byte
	args   *ConvertArgs
}

func (c *converter) blocks(n ast.Node) ([]Node, error) {
	var out []Node
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		nodes, err := c.block(child)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	return out, nil
}

func (c *converter) block(n ast.Node) ([]Node, error) {
	switch n := n.(type) {
	case *ast.Heading:
		inlines, err := c.inlines(n)
		if err != nil {
			return nil, err
		}
		return []Node{{
			Type:    "heading",
			Attrs:   map[string]any{"level": n.Level},
			Content: inlines,
		}}, nil

	case *ast.Paragraph:
		inlines, err := c.inlines(n)
		if err != nil {
			return nil, err
		}
		if len(inlines) == 0 {
			return nil, nil
		}
		return []Node{{Type: "paragraph", Content: inlines}}, nil

	case *ast.List:
		return c.list(n)

	case *ast.FencedCodeBlock:
		return c.fencedCode(n)

	case *ast.CodeBlock:
		var b strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			b.Write(line.Value(c.source))
		}
		return []Node{codeBlockNode("", b.String())}, nil

	case *ast.Blockquote:
		inner, err := c.blocks(n)
		if err != nil {
			return nil, err
		}
		return []Node{{Type: "blockquote", Content: inner}}, nil

	case *ast.ThematicBreak:
		return []Node{{Type: "rule"}}, nil

	case *east.Table:
		return c.table(n)

	case *ast.HTMLBlock:
		// Residue: do not invent storage macros from raw HTML.
		return nil, nil

	case *ast.TextBlock:
		// List item text blocks — treat as paragraph content.
		inlines, err := c.inlines(n)
		if err != nil {
			return nil, err
		}
		if len(inlines) == 0 {
			return nil, nil
		}
		return []Node{{Type: "paragraph", Content: inlines}}, nil

	default:
		// Unknown block: try children as blocks (e.g. document wrappers).
		if n.Kind() == ast.KindDocument {
			return c.blocks(n)
		}
		return c.blocks(n)
	}
}

func (c *converter) list(n *ast.List) ([]Node, error) {
	isTask := false
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		if _, checked, ok := taskCheckBox(item); ok {
			_ = checked
			isTask = true
			break
		}
	}

	var items []Node
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		li, ok := item.(*ast.ListItem)
		if !ok {
			continue
		}
		checked, hasTask := false, false
		if _, ckd, ok := taskCheckBox(li); ok {
			hasTask, checked = true, ckd
			isTask = true
		}
		body, err := c.blocks(li)
		if err != nil {
			return nil, err
		}
		// Drop empty leading paragraphs that only held the checkbox.
		body = stripEmptyParagraphs(body)
		if len(body) == 0 {
			body = []Node{{Type: "paragraph", Content: []Node{}}}
		}
		if isTask || hasTask {
			state := "TODO"
			if checked {
				state = "DONE"
			}
			items = append(items, Node{
				Type:    "taskItem",
				Attrs:   map[string]any{"state": state, "localId": fmt.Sprintf("task-%d", len(items)+1)},
				Content: body,
			})
		} else {
			items = append(items, Node{Type: "listItem", Content: body})
		}
	}

	if isTask {
		return []Node{{
			Type:    "taskList",
			Attrs:   map[string]any{"localId": "tasklist-1"},
			Content: items,
		}}, nil
	}
	if n.IsOrdered() {
		order := n.Start
		if order <= 0 {
			order = 1
		}
		return []Node{{
			Type:    "orderedList",
			Attrs:   map[string]any{"order": order},
			Content: items,
		}}, nil
	}
	return []Node{{Type: "bulletList", Content: items}}, nil
}

func (c *converter) fencedCode(n *ast.FencedCodeBlock) ([]Node, error) {
	lang := string(n.Language(c.source))
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		b.Write(line.Value(c.source))
	}
	src := b.String()
	// Trim a single trailing newline commonly present from the fence closer.
	src = strings.TrimSuffix(src, "\n")

	if strings.EqualFold(lang, "mermaid") {
		return []Node{c.mermaidExtension(src)}, nil
	}
	// Math residue: keep latex/math fences as codeBlock with language.
	return []Node{codeBlockNode(lang, src)}, nil
}

func (c *converter) mermaidExtension(source string) Node {
	key := c.args.mermaidKey()
	// extension (not codeBlock): source preserved for Confluence Mermaid macros.
	// Body placement varies by Marketplace app; parameters.macroParams.source
	// is a stable, testable carrier; bodied content mirrors the source too.
	return Node{
		Type: "bodiedExtension",
		Attrs: map[string]any{
			"extensionType": "com.atlassian.confluence.macro.core",
			"extensionKey":  key,
			"layout":        "default",
			"parameters": map[string]any{
				"macroParams": map[string]any{
					"source": map[string]any{"value": source},
				},
			},
		},
		Content: []Node{
			{
				Type: "codeBlock",
				Attrs: map[string]any{
					// Language is not "mermaid" on a top-level fence substitute;
					// this is body of the extension only.
					"language": "text",
				},
				Content: []Node{{Type: "text", Text: source}},
			},
		},
	}
}

func codeBlockNode(lang, src string) Node {
	n := Node{
		Type:    "codeBlock",
		Content: []Node{{Type: "text", Text: src}},
	}
	if lang != "" {
		n.Attrs = map[string]any{"language": lang}
	}
	return n
}

func (c *converter) table(n *east.Table) ([]Node, error) {
	var rows []Node
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		switch row := row.(type) {
		case *east.TableHeader:
			cells, err := c.tableCells(row, true)
			if err != nil {
				return nil, err
			}
			rows = append(rows, Node{Type: "tableRow", Content: cells})
		case *east.TableRow:
			cells, err := c.tableCells(row, false)
			if err != nil {
				return nil, err
			}
			rows = append(rows, Node{Type: "tableRow", Content: cells})
		}
	}
	return []Node{{
		Type:    "table",
		Attrs:   map[string]any{"isNumberColumnEnabled": false, "layout": "default"},
		Content: rows,
	}}, nil
}

func (c *converter) tableCells(row ast.Node, header bool) ([]Node, error) {
	var cells []Node
	for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
		tc, ok := cell.(*east.TableCell)
		if !ok {
			continue
		}
		// Table cells hold block content in goldmark (often paragraphs).
		inner, err := c.blocks(tc)
		if err != nil {
			return nil, err
		}
		if len(inner) == 0 {
			// Fallback: treat as inlines in a paragraph.
			inlines, err := c.inlines(tc)
			if err != nil {
				return nil, err
			}
			inner = []Node{{Type: "paragraph", Content: inlines}}
		}
		typ := "tableCell"
		if header {
			typ = "tableHeader"
		}
		cells = append(cells, Node{Type: typ, Attrs: map[string]any{}, Content: inner})
	}
	return cells, nil
}

// taskCheckBox reports whether a list item begins with a GFM task checkbox.
func taskCheckBox(n ast.Node) (node ast.Node, checked bool, ok bool) {
	li, isLI := n.(*ast.ListItem)
	if !isLI {
		return nil, false, false
	}
	// Checkbox is an inline under the first paragraph/text block.
	var find func(ast.Node) (*east.TaskCheckBox, bool)
	find = func(x ast.Node) (*east.TaskCheckBox, bool) {
		for c := x.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*east.TaskCheckBox); ok {
				return t, true
			}
			if t, ok := find(c); ok {
				return t, true
			}
		}
		return nil, false
	}
	t, found := find(li)
	if !found {
		return nil, false, false
	}
	return t, t.IsChecked, true
}

func stripEmptyParagraphs(nodes []Node) []Node {
	out := nodes[:0]
	for _, n := range nodes {
		if n.Type == "paragraph" && len(n.Content) == 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

type markStack []Mark

func (c *converter) inlines(n ast.Node) ([]Node, error) {
	var out []Node
	var err error
	out, err = c.walkInlines(n, nil, out)
	return out, err
}

func (c *converter) walkInlines(n ast.Node, marks markStack, out []Node) ([]Node, error) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch ch := child.(type) {
		case *ast.Text:
			seg := ch.Segment
			s := string(seg.Value(c.source))
			if ch.SoftLineBreak() {
				s += " "
			}
			if ch.HardLineBreak() {
				if s != "" {
					out = append(out, textNode(s, marks))
				}
				out = append(out, Node{Type: "hardBreak"})
				continue
			}
			if s == "" {
				continue
			}
			out = append(out, textNode(s, marks))

		case *ast.String:
			out = append(out, textNode(string(ch.Value), marks))

		case *ast.CodeSpan:
			inner := c.codeSpanText(ch)
			m := append(marks[:len(marks):len(marks)], Mark{Type: "code"})
			out = append(out, textNode(inner, m))

		case *east.TaskCheckBox:
			// Handled at list-item level; do not emit as text.
			continue

		case *ast.Emphasis:
			mark := Mark{Type: "em"}
			if ch.Level >= 2 {
				mark.Type = "strong"
			}
			var err error
			out, err = c.walkInlines(ch, append(marks, mark), out)
			if err != nil {
				return out, err
			}

		case *east.Strikethrough:
			var err error
			out, err = c.walkInlines(ch, append(marks, Mark{Type: "strike"}), out)
			if err != nil {
				return out, err
			}

		case *ast.Link:
			href := string(ch.Destination)
			title := string(ch.Title)
			attrs := map[string]any{"href": href}
			if title != "" {
				attrs["title"] = title
			}
			m := append(marks[:len(marks):len(marks)], Mark{Type: "link", Attrs: attrs})
			var err error
			out, err = c.walkInlines(ch, m, out)
			if err != nil {
				return out, err
			}

		case *ast.Image:
			nodes, err := c.image(ch, marks)
			if err != nil {
				return out, err
			}
			out = append(out, nodes...)

		case *ast.AutoLink:
			href := string(ch.URL(c.source))
			label := string(ch.Label(c.source))
			if label == "" {
				label = href
			}
			m := append(marks[:len(marks):len(marks)], Mark{Type: "link", Attrs: map[string]any{"href": href}})
			out = append(out, textNode(label, m))

		case *ast.RawHTML:
			// Residue: skip raw HTML tags; keep nothing.
			continue

		default:
			var err error
			out, err = c.walkInlines(ch, marks, out)
			if err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func (c *converter) codeSpanText(n *ast.CodeSpan) string {
	var b strings.Builder
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			b.Write(t.Segment.Value(c.source))
		}
	}
	return b.String()
}

func (c *converter) image(n *ast.Image, marks markStack) ([]Node, error) {
	dest := string(n.Destination)
	alt := c.imageAlt(n)
	policy := c.args.imagePolicy()

	switch policy {
	case ImagePolicyFail:
		return nil, fmt.Errorf("adf: image rejected by ImagePolicyFail: %s", dest)

	case ImagePolicyLink:
		href := dest
		label := alt
		if label == "" {
			label = dest
		}
		m := append(marks[:len(marks):len(marks)], Mark{Type: "link", Attrs: map[string]any{"href": href}})
		// Explicit policy: demotion to linked text is intentional.
		return []Node{textNode(label, m)}, nil

	case ImagePolicyExternal:
		if !isHTTPURL(dest) {
			return nil, fmt.Errorf("adf: ImagePolicyExternal requires http(s) URL, got %q", dest)
		}
		if isSVGTarget(dest) {
			// SVG over http(s) uses the same external media node as raster images.
			return []Node{externalMedia(dest, alt)}, nil
		}
		return []Node{externalMedia(dest, alt)}, nil

	default:
		return nil, fmt.Errorf("adf: unknown ImagePolicy %q", policy)
	}
}

func (c *converter) imageAlt(n *ast.Image) string {
	var b strings.Builder
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			b.Write(t.Segment.Value(c.source))
		}
	}
	return b.String()
}

func externalMedia(href, alt string) Node {
	media := Node{
		Type: "media",
		Attrs: map[string]any{
			"type": "external",
			"url":  href,
		},
	}
	if alt != "" {
		media.Attrs["alt"] = alt
	}
	return Node{
		Type:    "mediaSingle",
		Attrs:   map[string]any{"layout": "center"},
		Content: []Node{media},
	}
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func isSVGTarget(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return strings.HasSuffix(strings.ToLower(s), ".svg")
	}
	return strings.EqualFold(path.Ext(u.Path), ".svg")
}

func textNode(s string, marks markStack) Node {
	n := Node{Type: "text", Text: s}
	if len(marks) > 0 {
		n.Marks = append([]Mark(nil), marks...)
	}
	return n
}
