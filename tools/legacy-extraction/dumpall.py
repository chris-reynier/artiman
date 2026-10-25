"""Extract raw text of every .doc/.tmp under the Artiman folder into a JSON file."""
import hashlib
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from doctext import doc_text  # noqa: E402

root, out = sys.argv[1], sys.argv[2]
rows = []
for dp, _, fns in os.walk(root):
    for fn in sorted(fns):
        if not fn.lower().endswith((".doc", ".tmp")):
            continue
        p = os.path.join(dp, fn)
        raw = open(p, "rb").read()
        rec = {"folder": os.path.relpath(dp, root), "file": fn, "size": len(raw),
               "sha1": hashlib.sha1(raw).hexdigest()}
        try:
            rec["text"] = doc_text(p)
        except Exception as e:  # noqa: BLE001
            rec["error"] = repr(e)
        rows.append(rec)
json.dump(rows, open(out, "w", encoding="utf-8"), ensure_ascii=False, indent=0)
print(len(rows), "files,", sum("error" in r for r in rows), "errors")
for r in rows:
    if "error" in r:
        print(r["folder"], r["file"], r["error"])
