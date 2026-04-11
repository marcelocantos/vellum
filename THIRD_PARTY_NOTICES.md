# Third-Party Notices

Vellum binaries statically link the following Go modules at build time.
Each is redistributed under its own license; that license text is
available at the URL shown.

This file is maintained as a best-effort attribution of the compiled
binary's direct and transitive dependencies (as reported by
`go-licenses report ./cmd/vellum`). It does not list test-only
dependencies.

## Apache License 2.0

- **github.com/modelcontextprotocol/go-sdk** v1.5.0 —
  https://github.com/modelcontextprotocol/go-sdk/blob/v1.5.0/LICENSE
- **gopkg.in/yaml.v2** v2.3.0 —
  https://github.com/go-yaml/yaml/blob/v2.3.0/LICENSE

  `gopkg.in/yaml.v2` ships a `NOTICE` file. Its content, reproduced
  verbatim as required by Apache License 2.0 §4(d):

  > Copyright 2011-2016 Canonical Ltd.
  >
  > Licensed under the Apache License, Version 2.0 (the "License");
  > you may not use this file except in compliance with the License.
  > You may obtain a copy of the License at
  >
  >     http://www.apache.org/licenses/LICENSE-2.0
  >
  > Unless required by applicable law or agreed to in writing, software
  > distributed under the License is distributed on an "AS IS" BASIS,
  > WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  > See the License for the specific language governing permissions and
  > limitations under the License.

## MIT License

- **github.com/alecthomas/chroma/v2** v2.23.1 —
  https://github.com/alecthomas/chroma/blob/v2.23.1/COPYING
- **github.com/dlclark/regexp2** v1.11.5 —
  https://github.com/dlclark/regexp2/blob/v1.11.5/LICENSE
- **github.com/google/jsonschema-go** v0.4.2 —
  https://github.com/google/jsonschema-go/blob/v0.4.2/LICENSE
- **github.com/segmentio/asm** v1.1.3 —
  https://github.com/segmentio/asm/blob/v1.1.3/LICENSE
- **github.com/segmentio/encoding** v0.5.4 —
  https://github.com/segmentio/encoding/blob/v0.5.4/LICENSE
- **github.com/yuin/goldmark** v1.8.2 —
  https://github.com/yuin/goldmark/blob/v1.8.2/LICENSE
- **github.com/yuin/goldmark-highlighting/v2** v2.0.0-20230729083705-37449abec8cc —
  https://github.com/yuin/goldmark-highlighting/blob/37449abec8cc/LICENSE
- **github.com/yuin/goldmark-meta** v1.1.0 —
  https://github.com/yuin/goldmark-meta/blob/v1.1.0/LICENSE

## BSD 3-Clause License

- **github.com/yosida95/uritemplate/v3** v3.0.2 —
  https://github.com/yosida95/uritemplate/blob/v3.0.2/LICENSE
- **golang.org/x/oauth2** v0.35.0 —
  https://cs.opensource.google/go/x/oauth2/+/v0.35.0:LICENSE
- **golang.org/x/sys** v0.41.0 —
  https://cs.opensource.google/go/x/sys/+/v0.41.0:LICENSE

## Runtime dependencies (not linked)

Vellum invokes the following tools via `exec.LookPath` at runtime.
They are distributed separately, not bundled into the vellum binary,
and their licenses apply to their own distributions, not to vellum.

- **Prince** — https://www.princexml.com/ (proprietary; free for non-commercial use)
- **Node.js** — https://nodejs.org/ (MIT)
- **KaTeX** — https://katex.org/ (MIT)
- **mermaid-cli** — https://github.com/mermaid-js/mermaid-cli (MIT)
