# Viewer smoke

Use this file to check live reload and editable task lists.

1. Leave this tab open.
2. Toggle a checkbox below by clicking the box or tabbing to it and pressing Space (not the text). First click asks for consent (this session or always). The source on disk should flip `[ ]` ↔ `[x]`. The toggle itself should stay in place without a full reload. An external save may reload; keyboard focus must return to the same box. Clicking the label text must not toggle.
3. The top bar shows the Vellum mark (a link to the homepage), the contents toggle and this file's name on the left; scroll down and the name becomes the document's title, and a click on it returns to the top. The contents panel marks the section being read. On the right are icons for edit (pencil), copy, PDF (a page lettered PDF), Finder (its real icon) and the theme. Press the pencil (or `e`). The editor should open showing what the page showed at the top (scroll partway into a paragraph or code block first), in the page's theme. Select a phrase that spans **bold** text or a link first and it should arrive selected in the source. Esc without scrolling should put the page back exactly where it was, even with text selected in the editor; with the search panel open (⌘F), the first Esc closes only the panel. Scroll the editor elsewhere, add a line, press ⌘S, then Esc: the view should refresh with the line, showing what the editor showed at the top. With unsaved changes, Esc should ask before closing.
4. Scroll to **Deep section**, then edit this file in an editor (change the line below) and save. The tab should update and stay near that heading.

Edit me for live reload: `unchanged`.

## Packing

- [ ] Passport
- [x] Tickets
- [ ] Charger
- [ ] Headphones

## Groceries

- [ ] Oat milk
- [ ] Tomatoes
- [x] Coffee
  - [ ] Beans
  - [ ] Filters
- [ ] Bread

## Deep section

Scroll here before saving an edit above, so you can see heading-anchor restore.

Lorem ipsum dolor sit amet, consectetur adipiscing elit. Vestibulum ante
ipsum primis in faucibus orci luctus et ultrices posuere cubilia curae.

### Notes

- [ ] Confirm heartbeat: leave the tab idle; it should stay connected
      (protocol ping every five minutes; you will not see it).
- [ ] Confirm rename: rename this file while the tab is open —
      the tab should open the file at its new path.
- [ ] Confirm delete: delete this file while the tab is open —
      the rendered page stays, with a notice that the file was deleted.
