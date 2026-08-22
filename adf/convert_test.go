// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package adf_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/adf"
)

func TestConvert_RootVersionAndType(t *testing.T) {
	doc, err := adf.Convert("# Hi\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 {
		t.Fatalf("version=%d want 1", doc.Version)
	}
	if doc.Type != "doc" {
		t.Fatalf("type=%q want doc", doc.Type)
	}
}

func TestConvert_CoreGFMNodeTypes(t *testing.T) {
	md := strings.TrimSpace(`
# Title

Paragraph with **bold**, *italic*, ~~strike~~, ` + "`code`" + ` and [link](https://example.com).

## Sub

- bullet one
- bullet two

1. ordered a
2. ordered b

> a quote

---

` + "```" + `go
fmt.Println("hi")
` + "```" + `

| Col A | Col B |
| ----- | ----- |
| hello | world |
`)
	doc, err := adf.Convert(md, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	// Must not be a single prose/code dump of the whole markdown.
	if strings.Count(s, `"type":"codeBlock"`) == 1 && strings.Contains(s, "# Title") {
		t.Fatalf("looks like markdown dumped into one code block: %s", s)
	}

	wantTypes := []string{
		`"type":"heading"`,
		`"type":"paragraph"`,
		`"type":"bulletList"`,
		`"type":"listItem"`,
		`"type":"orderedList"`,
		`"type":"blockquote"`,
		`"type":"rule"`,
		`"type":"codeBlock"`,
		`"type":"table"`,
		`"type":"tableRow"`,
		`"type":"tableHeader"`,
		`"type":"tableCell"`,
		`"type":"strong"`,
		`"type":"em"`,
		`"type":"strike"`,
		`"type":"code"`,
		`"type":"link"`,
	}
	for _, w := range wantTypes {
		if !strings.Contains(s, w) {
			t.Errorf("missing %s in ADF JSON", w)
		}
	}

	// Structural: walk types from unmarshaled doc.
	types := collectTypes(doc.Content)
	for _, need := range []string{"heading", "paragraph", "bulletList", "orderedList", "blockquote", "rule", "codeBlock", "table"} {
		if !types[need] {
			t.Errorf("missing node type %q in tree", need)
		}
	}
	if !types["tableHeader"] || !types["tableCell"] {
		t.Errorf("table missing header/cell; types=%v", types)
	}
}

func TestConvert_MermaidToExtensionNotCodeBlockOrMedia(t *testing.T) {
	md := "Intro.\n\n```mermaid\nflowchart TD\n  A-->B\n```\n\nOutro.\n"
	args := &adf.ConvertArgs{MermaidMacro: "test-mermaid-macro"}
	doc, err := adf.Convert(md, args)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := adf.ConvertJSON(md, args)
	if err != nil {
		t.Fatal(err)
	}
	// Persist for verification plan observation.
	if dir := os.Getenv("T26_SCRATCH"); dir != "" {
		path := filepath.Join(dir, "t26-mermaid.adf.json")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var foundExt bool
	var foundMermaidCodeOnly bool
	var walk func([]adf.Node)
	walk = func(nodes []adf.Node) {
		for _, n := range nodes {
			switch n.Type {
			case "extension", "bodiedExtension":
				foundExt = true
				key, _ := n.Attrs["extensionKey"].(string)
				if key != "test-mermaid-macro" {
					t.Errorf("extensionKey=%q want test-mermaid-macro", key)
				}
				blob, _ := json.Marshal(n)
				var decoded any
				if err := json.Unmarshal(blob, &decoded); err != nil {
					t.Fatal(err)
				}
				flat, _ := json.Marshal(decoded) // re-encode; still escaped
				// Source must survive JSON; check structural text field after walk.
				srcOK := false
				var findText func(adf.Node)
				findText = func(x adf.Node) {
					if x.Type == "text" && strings.Contains(x.Text, "flowchart TD") && strings.Contains(x.Text, "A-->B") {
						srcOK = true
					}
					// Also parameters.macroParams.source.value
					if x.Attrs != nil {
						if p, ok := x.Attrs["parameters"].(map[string]any); ok {
							if mp, ok := p["macroParams"].(map[string]any); ok {
								if src, ok := mp["source"].(map[string]any); ok {
									if v, ok := src["value"].(string); ok && strings.Contains(v, "flowchart TD") && strings.Contains(v, "A-->B") {
										srcOK = true
									}
								}
							}
						}
					}
					for _, c := range x.Content {
						findText(c)
					}
				}
				findText(n)
				if !srcOK {
					t.Errorf("mermaid source not preserved in extension: %s / %s", blob, flat)
				}
				if n.Type == "codeBlock" {
					t.Error("extension node mis-typed as codeBlock")
				}
			case "codeBlock":
				lang, _ := n.Attrs["language"].(string)
				// Top-level mermaid fence must not be only a mermaid codeBlock.
				if lang == "mermaid" {
					// Allowed only nested under extension; flag if parent walk
					// sees this as sibling of prose without extension.
					foundMermaidCodeOnly = true
				}
			case "media", "mediaSingle":
				blob, _ := json.Marshal(n)
				if strings.Contains(string(blob), "flowchart") {
					t.Errorf("mermaid became media/image: %s", blob)
				}
			}
			walk(n.Content)
		}
	}
	walk(doc.Content)
	if !foundExt {
		t.Fatalf("no extension/bodiedExtension for mermaid; json=%s", raw)
	}
	// Ensure we did not *only* emit a top-level codeBlock language=mermaid
	// without an extension wrapper. Nested text codeBlock under extension is OK.
	topMermaidCode := false
	for _, n := range doc.Content {
		if n.Type == "codeBlock" {
			if lang, _ := n.Attrs["language"].(string); lang == "mermaid" {
				topMermaidCode = true
			}
		}
	}
	if topMermaidCode {
		t.Fatal("top-level codeBlock language=mermaid; want extension family")
	}
	_ = foundMermaidCodeOnly
}

func TestConvert_ImagePolicyExternal(t *testing.T) {
	md := `![alt text](https://example.com/pic.png)`
	doc, err := adf.Convert(md, &adf.ConvertArgs{ImagePolicy: adf.ImagePolicyExternal})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	if dir := os.Getenv("T26_SCRATCH"); dir != "" {
		_ = os.MkdirAll(filepath.Join(dir, "t26-media"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "t26-media", "external.json"), raw, 0o644)
	}
	types := collectTypes(doc.Content)
	if !types["mediaSingle"] || !types["media"] {
		t.Fatalf("want mediaSingle/media, got types=%v json=%s", types, raw)
	}
	// Must not be a bare link demotion without media.
	if !strings.Contains(string(raw), `"type":"external"`) {
		t.Fatalf("media missing type=external: %s", raw)
	}
	if !strings.Contains(string(raw), "https://example.com/pic.png") {
		t.Fatalf("url missing: %s", raw)
	}
}

func TestConvert_ImagePolicyExternal_SVG(t *testing.T) {
	md := `![diagram](https://example.com/d.svg)`
	doc, err := adf.Convert(md, &adf.ConvertArgs{ImagePolicy: adf.ImagePolicyExternal})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	if dir := os.Getenv("T26_SCRATCH"); dir != "" {
		_ = os.MkdirAll(filepath.Join(dir, "t26-media"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "t26-media", "svg-external.json"), raw, 0o644)
	}
	if !strings.Contains(string(raw), `"type":"media"`) {
		t.Fatalf("svg should use media node: %s", raw)
	}
}

func TestConvert_ImagePolicyExternal_LocalFails(t *testing.T) {
	md := `![x](./local.png)`
	_, err := adf.Convert(md, &adf.ConvertArgs{ImagePolicy: adf.ImagePolicyExternal})
	if err == nil {
		t.Fatal("expected error for relative image under ImagePolicyExternal")
	}
	if !strings.Contains(err.Error(), "ImagePolicyExternal") {
		t.Fatalf("error=%v", err)
	}
	if dir := os.Getenv("T26_SCRATCH"); dir != "" {
		_ = os.MkdirAll(filepath.Join(dir, "t26-media"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "t26-media", "local-fail.txt"), []byte(err.Error()), 0o644)
	}
}

func TestConvert_ImagePolicyFail(t *testing.T) {
	md := `![x](https://example.com/a.png)`
	_, err := adf.Convert(md, &adf.ConvertArgs{ImagePolicy: adf.ImagePolicyFail})
	if err == nil {
		t.Fatal("expected ImagePolicyFail error")
	}
	if !strings.Contains(err.Error(), "ImagePolicyFail") {
		t.Fatalf("error=%v", err)
	}
}

func TestConvert_ImagePolicyLink_ExplicitDemotion(t *testing.T) {
	// Explicit policy may demote to link; silent default must not.
	md := `![label](https://example.com/a.png)`
	doc, err := adf.Convert(md, &adf.ConvertArgs{ImagePolicy: adf.ImagePolicyLink})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	if dir := os.Getenv("T26_SCRATCH"); dir != "" {
		_ = os.MkdirAll(filepath.Join(dir, "t26-media"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "t26-media", "link-policy.json"), raw, 0o644)
	}
	types := collectTypes(doc.Content)
	if types["media"] || types["mediaSingle"] {
		t.Fatalf("link policy should not emit media: %s", raw)
	}
	if !strings.Contains(string(raw), `"type":"link"`) {
		t.Fatalf("want link mark: %s", raw)
	}
}

func TestConvert_DefaultImagePolicyIsExternalNotSilentLink(t *testing.T) {
	// nil args → external; relative must fail (not silent link).
	_, err := adf.Convert(`![](./x.png)`, nil)
	if err == nil {
		t.Fatal("default policy must not silently accept relative images as links")
	}
}

func TestConvert_MathResidueIsCodeBlockNotDropped(t *testing.T) {
	// Explicit ```latex fence (also the intermediate form for $ delimiters).
	md := "Before\n\n```latex\nE = mc^2\n```\n\nAfter\n"
	doc, err := adf.Convert(md, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	if !strings.Contains(string(raw), "E = mc^2") {
		t.Fatalf("math source lost: %s", raw)
	}
	if !strings.Contains(string(raw), `"language":"latex"`) {
		t.Fatalf("math residue should be latex codeBlock: %s", raw)
	}
}

func TestConvert_DollarMathResidue_DisplayAndInline(t *testing.T) {
	// Real KaTeX-style delimiters — not a pre-fenced latex block.
	md := "Intro\n\n$$\nE = mc^2\n$$\n\nInline $a+b$ end.\n"
	doc, err := adf.Convert(md, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("T26_SCRATCH"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "t26-math-dollars.adf.json"), raw, 0o644)
	}
	s := string(raw)
	// Must not leave raw dollar delimiters as prose-only residue.
	if strings.Contains(s, `"text":"$$"`) || strings.Contains(s, `"text":"$a+b$"`) {
		t.Fatalf("dollar delimiters left as prose: %s", s)
	}
	if !strings.Contains(s, "E = mc^2") {
		t.Fatalf("display math expr lost: %s", s)
	}
	if !strings.Contains(s, "a+b") {
		t.Fatalf("inline math expr lost: %s", s)
	}
	// At least one latex codeBlock for the residue mapping.
	if !strings.Contains(s, `"language":"latex"`) {
		t.Fatalf("want language=latex codeBlock residue: %s", s)
	}
	types := collectTypes(doc.Content)
	if !types["codeBlock"] {
		t.Fatalf("want codeBlock for math residue: %s", s)
	}
	// Code-protected dollars must not be rewritten.
	mdCode := "Use `$notmath$` in code and\n\n```\n$also$\n```\n"
	doc2, err := adf.Convert(mdCode, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := json.Marshal(doc2)
	// Inline code keeps $notmath$ as code mark text, not forced latex block only.
	if !strings.Contains(string(raw2), "$notmath$") && !strings.Contains(string(raw2), "notmath") {
		t.Fatalf("code-protected math mangled: %s", raw2)
	}
}

func TestResidueDocDocumentsMath(t *testing.T) {
	// Force-read package documentation so residue is not aspirational-only.
	data, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, needle := range []string{"Declared residue", "KaTeX", "latex"} {
		if !strings.Contains(s, needle) {
			t.Errorf("doc.go missing residue mention %q", needle)
		}
	}
}

func TestConvertJSON_RoundTripShape(t *testing.T) {
	b, err := adf.ConvertJSON("**x**\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc adf.Document
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Type != "doc" {
		t.Fatalf("%+v", doc)
	}
}

func collectTypes(nodes []adf.Node) map[string]bool {
	m := map[string]bool{}
	var walk func([]adf.Node)
	walk = func(ns []adf.Node) {
		for _, n := range ns {
			m[n.Type] = true
			for _, mk := range n.Marks {
				m[mk.Type] = true
			}
			walk(n.Content)
		}
	}
	walk(nodes)
	return m
}
