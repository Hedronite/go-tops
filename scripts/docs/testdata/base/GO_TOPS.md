# Fixture protocol

## 1. Catalog (draft: 3 laws, `G-01`…`G-03`)

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-01 | First law | R | an | ✓ |
| G-02 | Retired into G-01: second law | R | an | ✓ |
| G-03 | Third law with a pipe \| in it | A | at | |

The report states "1 of 3 laws judged", and 2 active laws remain.
Rust-TOPS has a sibling suite with 53 laws and 15 judged.

**v0.1 target:** judge 1 law mechanically (the ✓ rows), mirroring Rust-TOPS's
approach of shipping a minority of laws.

| Suite | Laws |
|---|---|
| Sibling | Bend2 (53 laws, 15 judged) |
| This one | native multichecker (3 laws); no Bend or Lean layer |

```json
{ "catalog": 3, "judged": 1, "not_evaluated": 1 }
```

### 5.3 Profile file shape

Prose only: no example block here.

## 9. Configuration

Prose only: no example block here.

### 12.3 Report

Prose only: no example block here.

## 10. After the example sections

Prose only.
