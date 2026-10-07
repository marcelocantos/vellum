# C06 — Rich structure and declared fallbacks

The purpose is to catch non-layout regressions during table and media fixes.
The source order and words matter more than exact typography.

## Paragraphs and links

This paragraph contains **strong text**, *emphasis*, ~~deleted text~~,
`inline code`, and a [link with a query string](https://example.com/docs?q=adf&mode=full).

> A quotation starts here.
>
> Its second paragraph remains inside the quotation.

## Lists and tasks

1. Prepare the source document.
2. Upload assets before updating the page.
   1. Keep the attachment IDs.
   2. Check the page version.
3. Read the published result.

- [x] A completed task
- [ ] An open task

## Code and equations

```go
func publish(pageID string, version int) error {
    return fmt.Errorf("page %s version %d requires review", pageID, version)
}
```

Inline math $E = mc^2$ and display math should remain visible even if they
use the declared LaTeX-code fallback:

$$
\int_0^1 x^2\,dx = \frac{1}{3}
$$

## Footnote and raw HTML probes

This sentence has a footnote reference.[^layout]

[^layout]: The note records why a paragraph changed after publication.

<aside>This raw HTML aside probes the declared unsupported path.</aside>

The last sentence must still appear after the unsupported constructs.
