// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/marcelocantos/vellum/convert"
)

// retiredToolNames are tools the server exposed before the four-tool
// split collapsed into a single convert tool (🎯T14). Naming them in the
// server instructions or in a tool's documentation sends callers after
// an interface that no longer exists (🎯T24).
var retiredToolNames = []string{"convert_to_clipboard", "convert_from_clipboard", "import"}

// advertisement is one piece of caller-visible documentation read off
// the wire, labelled by where the client saw it.
type advertisement struct {
	where string
	text  string
}

// TestAdvertisedToolNamesResolve asserts that every tool name the server
// advertises resolves to a tool it actually registers. It connects a
// real server over an in-memory transport and reads the instructions and
// tool documentation exactly as a client does, so it checks the shipped
// strings rather than a copy of them.
func TestAdvertisedToolNamesResolve(t *testing.T) {
	ads, tools := advertisements(t)

	registered := map[string]bool{}
	for _, tool := range tools {
		registered[tool.Name] = true
	}
	vocabulary := schemaVocabulary()

	for _, ad := range ads {
		for _, name := range identifierMentions(ad.text) {
			if registered[name] || vocabulary[name] {
				continue
			}
			t.Errorf("%s names %q, which is neither a registered tool (%s) "+
				"nor a schema field or media name", ad.where, name, strings.Join(sortedKeys(registered), ", "))
		}
	}
}

// TestRetiredToolNamesNotAdvertised pins the specific regression: the
// pre-🎯T14 tool names must be neither registered nor advertised.
func TestRetiredToolNamesNotAdvertised(t *testing.T) {
	ads, tools := advertisements(t)

	for _, retired := range retiredToolNames {
		for _, tool := range tools {
			if tool.Name == retired {
				t.Errorf("retired tool %q is registered again", retired)
			}
		}
		for _, ad := range ads {
			for _, name := range identifierMentions(ad.text) {
				if name == retired {
					t.Errorf("%s advertises retired tool %q", ad.where, retired)
				}
			}
		}
	}
}

// TestServerExposesOnlyConvert pins the tool surface itself, so the
// documentation check above cannot be satisfied by re-adding a tool.
func TestServerExposesOnlyConvert(t *testing.T) {
	_, tools := advertisements(t)

	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	if want := []string{"convert"}; !reflect.DeepEqual(names, want) {
		t.Errorf("exposed tools = %v, want %v", names, want)
	}
}

// advertisements connects a server over an in-memory transport and
// returns every caller-visible documentation string alongside the tool
// list the client received.
func advertisements(t *testing.T) ([]advertisement, []*mcp.Tool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer("test", nil, "").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "vellum-doc-check", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting client: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	ads := []advertisement{{where: "server instructions", text: session.InitializeResult().Instructions}}
	for _, tool := range res.Tools {
		ads = append(ads,
			advertisement{where: "tool " + tool.Name + " title", text: tool.Title},
			advertisement{where: "tool " + tool.Name + " description", text: tool.Description},
		)
	}
	return ads, res.Tools
}

var (
	// identifierShape is the token shape a tool name can take.
	identifierShape = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	// quotedToken matches backtick- or double-quote-delimited text, the
	// two ways documentation presents a name as an identifier rather
	// than as an English word.
	quotedToken = regexp.MustCompile("`([^`]+)`|\"([^\"]+)\"")
	// snakeToken matches bare snake_case, which is an identifier
	// wherever it appears — no quoting needed.
	snakeToken = regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z0-9]+)+`)
)

// identifierMentions returns the tokens in text that are presented as
// identifiers: quoted, backticked, or snake_case. Unquoted prose is
// excluded deliberately — "rich import via pandoc" describes a document
// path, not a tool called import.
func identifierMentions(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(token string) {
		if !identifierShape.MatchString(token) || seen[token] {
			return
		}
		seen[token] = true
		out = append(out, token)
	}
	for _, m := range quotedToken.FindAllStringSubmatch(text, -1) {
		add(m[1])
		add(m[2])
	}
	for _, m := range snakeToken.FindAllString(text, -1) {
		add(m)
	}
	sort.Strings(out)
	return out
}

// schemaVocabulary is the set of identifiers documentation may name that
// are not tools: request and response field names, and media values. It
// is derived from the schema types themselves so it cannot drift into a
// hand-maintained excuse list.
func schemaVocabulary() map[string]bool {
	vocabulary := map[string]bool{}
	for _, media := range []convert.Media{
		convert.MediaFile, convert.MediaContent, convert.MediaClipboard, convert.MediaFileReference,
	} {
		vocabulary[string(media)] = true
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(ConvertInput{}),
		reflect.TypeOf(ConvertOutput{}),
		reflect.TypeOf(Endpoint{}),
		reflect.TypeOf(FilePair{}),
		reflect.TypeOf(convert.Style{}),
	} {
		for i := range typ.NumField() {
			name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			if name != "" && name != "-" {
				vocabulary[name] = true
			}
		}
	}
	return vocabulary
}

func sortedKeys(set map[string]bool) []string {
	var keys []string
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
