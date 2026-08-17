// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package adf

// Document is a version-1 ADF root document.
type Document struct {
	Version int    `json:"version"`
	Type    string `json:"type"`
	Content []Node `json:"content"`
}

// Node is any ADF block or inline node.
type Node struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Content []Node         `json:"content,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Mark is an ADF text mark (strong, em, code, link, strike, …).
type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// ImagePolicy names how Markdown images (and SVG image targets) are handled.
// A policy is always required so images are never silently demoted to bare
// links without an explicit choice.
type ImagePolicy string

const (
	// ImagePolicyExternal maps http(s) image URLs to mediaSingle/media with
	// type "external". Relative or non-http destinations fail.
	ImagePolicyExternal ImagePolicy = "external"

	// ImagePolicyLink maps images to linked text (alt or URL) with an explicit
	// link mark — only when the caller chooses this demotion.
	ImagePolicyLink ImagePolicy = "link"

	// ImagePolicyFail rejects any image node with a non-nil error.
	ImagePolicyFail ImagePolicy = "fail"
)

// ConvertArgs configures Markdown → ADF conversion.
// Pass nil to Convert for defaults (external images, mermaid key "mermaid").
type ConvertArgs struct {
	// MermaidMacro is the Confluence macro extensionKey for ```mermaid fences.
	// Empty defaults to "mermaid" (tests and sites that install a matching app).
	MermaidMacro string

	// ImagePolicy controls image and SVG image-link handling. Empty defaults
	// to ImagePolicyExternal.
	ImagePolicy ImagePolicy

	// BaseDir is reserved for resolving relative image paths when an upload
	// policy is added; unused for external/link/fail in T26.
	BaseDir string
}

func (a *ConvertArgs) mermaidKey() string {
	if a != nil && a.MermaidMacro != "" {
		return a.MermaidMacro
	}
	return "mermaid"
}

func (a *ConvertArgs) imagePolicy() ImagePolicy {
	if a != nil && a.ImagePolicy != "" {
		return a.ImagePolicy
	}
	return ImagePolicyExternal
}
