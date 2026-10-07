# C02 — Table cell pressure

The first column is a short key. The last is prose. The middle columns contain
tokens that are awkward to wrap. None should vanish or overlap another cell.

| Key | Revision | Endpoint | Explanation |
| :--- | :--- | :--- | :--- |
| A | `v0.25.0` | `/wiki/api/v2/pages/782134` | A successful update keeps the page identity and increments its version. The explanation may wrap across lines. |
| B | `9a04d6a283a3a15fe5d62ca4c160546cf0a7dc3e` | `/wiki/api/v2/attachments/782135` | A long unbroken revision must remain recoverable, even if the table needs horizontal scrolling. |
| C | `ETIMEDOUT` | [Atlassian API reference](https://developer.atlassian.com/cloud/confluence/rest/v2/) | A **failed upload** must be visible to the caller; it must not be described as a successful page publish. |

Cells in this smaller table mix styles. The link should remain clickable and
the code literal should retain its punctuation.

| Check | Evidence | Status |
| :--- | :--- | :---: |
| Rich text | **Bold**, *emphasis*, ~~obsolete~~ | pass |
| Code | `width: 960px; overflow-x: auto;` | pass |
| Link | [Read the source](https://example.com/reference?section=table&view=wide) | review |

The paragraph after the tables must return to the normal reading width.
