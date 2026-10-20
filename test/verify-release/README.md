# verify-release fixtures

Inputs for `make verify-release` (`scripts/verify-release.sh`), the
end-to-end check of the *installed* release after `brew upgrade vellum`.

- `doc.md` — headings, a table, a fenced code block, a Mermaid diagram,
  and inline and display KaTeX math. Every assertion in the script keys
  off one of the marker words below, so edit both together.
- The DOCX sample is `convert/testdata/corpus/styled/styled.docx`
  (ground truth in its `manifest.yaml`).

Markers: `Verify Release Fixture`, `Second Heading`, `tableCellOmega`,
`codeSentinel42`, Mermaid node `MermaidNodeAlpha`, math `e^{i\pi}`.
