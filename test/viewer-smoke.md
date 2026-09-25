# Viewer smoke

Use this file to check live reload and editable task lists.

1. Leave this tab open.
2. Toggle a checkbox below by clicking the box or tabbing to it and pressing Space (not the text). First click asks for consent (this session or always). The source on disk should flip `[ ]` ↔ `[x]`. The toggle itself should stay in place without a full reload. An external save may reload; keyboard focus must return to the same box. Clicking the label text must not toggle.
3. Scroll to **Deep section**, then edit this file in an editor (change the line below) and save. The tab should update and stay near that heading.

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
- [ ] Confirm delete-safety: rename this file while the tab is open —
      the status line should error, not reload-loop.
