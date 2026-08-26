// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestHTTPHandler_ConvertOverStreamableHTTP connects a real MCP client
// over streamable HTTP and both lists tools and calls convert. This is
// the transport the brew-service daemon exposes at /mcp (🎯T31).
func TestHTTPHandler_ConvertOverStreamableHTTP(t *testing.T) {
	h := HTTPHandler("test", nil, "")
	if c, ok := h.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = c.Close() })
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "vellum-http-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connecting HTTP client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if len(names) != 1 || names[0] != "convert" {
		t.Fatalf("tools = %v, want [convert]", names)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "convert",
		Arguments: ConvertInput{
			From: &Endpoint{Media: "content", Content: "# Only\n"},
			To:   &Endpoint{Media: "content"},
		},
	})
	if err != nil {
		t.Fatalf("calling convert: %v", err)
	}
	if result.IsError {
		t.Fatalf("convert IsError: %s", toolText(result))
	}
	if !strings.Contains(toolText(result), "# Only") {
		t.Fatalf("convert text missing source:\n%s\nstructured=%v", toolText(result), result.StructuredContent)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("structured: %v", err)
	}
	if !strings.Contains(string(raw), "content") {
		t.Fatalf("structured output missing content field: %s", raw)
	}
}

// TestHTTPHandler_MountedAtMCPPath mirrors viewer.Server: the handler
// lives at /mcp on a mux that also has other routes. Clients must use
// the /mcp endpoint, not the origin root.
func TestHTTPHandler_MountedAtMCPPath(t *testing.T) {
	h := HTTPHandler("test", nil, "")
	if c, ok := h.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = c.Close() })
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", h)
	mux.Handle("/mcp/", h)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "vellum-mux-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connecting at /mcp: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "convert" {
		t.Fatalf("tools = %v", listed.Tools)
	}
}

func toolText(result *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
