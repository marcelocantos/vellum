# C01 — Table shapes

The short control should fit comfortably in the normal reading column.

| State | Count |
| :--- | ---: |
| Ready | 7 |
| Waiting | 12 |

The next table has deliberately unequal content. The identifier and result
columns should remain compact while the explanation receives most of the
width.

| ID | Result | Explanation | Owner |
| :--- | :---: | :--- | ---: |
| T27.3 | Pass | A local page binding was found, so the existing Confluence page should be updated without creating a duplicate. | Ana |
| T104.12 | Review | The source document moved between directories; the stored page identity should survive the path repair. | Bo |
| T9 | Fail | The attachment upload returned a usable error, and the publish attempt must leave the previous page version intact. | Cy |

This nine-column matrix needs more than paragraph width. On a narrow browser,
horizontal scrolling is acceptable; clipped labels and single-character
columns are not.

| Scenario | Source | Space | Page ID | Version | Asset | Policy | Result | Notes |
| :--- | :--- | :--- | ---: | ---: | :--- | :--- | :---: | :--- |
| A-001 | `docs/spec.md` | ENG | 782134 | 12 | diagram.svg | upload | pass | Diagram labels must remain readable. |
| A-002 | `reports/q3.md` | OPS | 782135 | 3 | chart.png | upload | review | The chart has a wide legend and should use the page width. |
| A-003 | `runbooks/restore.md` | SRE | 782136 | 28 | none | none | fail | A stale version must not overwrite new edits. |
| A-004 | `notes/team.md` | TEAM | 782137 | 7 | portrait.svg | upload | pass | The portrait should fit without becoming wider than the prose. |

The final table tests alignment without asking for extra width.

| Left label | Center flag | Right amount |
| :--- | :---: | ---: |
| one | yes | 1,234.50 |
| longer label | no | 8.00 |
