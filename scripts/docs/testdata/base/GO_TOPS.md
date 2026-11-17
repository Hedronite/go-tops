# Fixture protocol

## 1. Catalog (draft: 3 laws, `G-01`…`G-03`)

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-01 | First law | R | an | ✓ |
| G-02 | Retired into G-01: second law | R | an | ✓ |
| G-03 | Third law with a pipe \| in it | A | at | |

The report states "1 of 3 laws judged", and 2 active laws remain.
A sibling suite has 53 laws and 15 judged, and Rust-TOPS says so.

| Suite | Laws |
|---|---|
| Sibling | Bend2 (53 laws, 15 judged) |
| This one | native (3 laws) |

```json
{ "catalog": 3, "judged": 1 }
```
