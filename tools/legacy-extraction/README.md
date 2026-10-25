# Legacy extraction tools

Python scripts used on 2026-10-25 to rebuild the history of the old ArtiMan after its database reset.

| Script | Role |
|---|---|
| `doctext.py` | Extracts the text of a Word 97-2003 `.doc` through its piece table (no Word needed) |
| `dumpall.py` | Extracts every document of the old ArtiMan folder into `raw.json` |
| `parse.py` | Parses estimates/invoices (client, lines, totals) into `parsed.json` |
| `build.py` | Consolidates customers, links invoices to estimates, flags anomalies → `data.json` (input of ArtiMan Web) |
| `package.py` | Builds the static HTML history report |
| `read_db.py` / `gen_db.py` | Read / write the binary `ArtiManD.db` format of the old program (documented in `docs/legacy-format.md`) |

Run with `uv` and `olefile`:

```
uv venv .venv && uv pip install --python .venv olefile
.venv/Scripts/python -I dumpall.py <ArtiMan folder> raw.json
.venv/Scripts/python -I parse.py raw.json parsed.json
.venv/Scripts/python -I build.py parsed.json data.json
```
