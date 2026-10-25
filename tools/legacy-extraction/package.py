"""Render index.html from template + data and package it with the original documents."""
import json
import os
import shutil
import sys
import zipfile

scratch, src_root, out_dir = sys.argv[1], sys.argv[2], sys.argv[3]
data = json.load(open(os.path.join(scratch, "data.json"), encoding="utf-8"))
tpl = open(os.path.join(scratch, "template.html"), encoding="utf-8").read()
payload = json.dumps(data, ensure_ascii=False, separators=(",", ":")).replace("</", "<\\/")
html = tpl.replace("/*DATA*/", payload)

if os.path.isdir(out_dir):
    shutil.rmtree(out_dir)
os.makedirs(os.path.join(out_dir, "documents"))
open(os.path.join(out_dir, "index.html"), "w", encoding="utf-8").write(html)
paths = set()
for d in data["docs"]:
    paths.add(d["path"])
    paths.update(d["copies"])
for p in sorted(paths):
    dst = os.path.join(out_dir, "documents", p)
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    shutil.copy2(os.path.join(src_root, p), dst)

zpath = out_dir.rstrip("\\/") + ".zip"
with zipfile.ZipFile(zpath, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
    base = os.path.basename(out_dir.rstrip("\\/"))
    for dp, _, fns in os.walk(out_dir):
        for fn in fns:
            full = os.path.join(dp, fn)
            z.write(full, os.path.join(base, os.path.relpath(full, out_dir)))
print("html", len(html) // 1024, "KB; documents", len(paths), "; zip", os.path.getsize(zpath) // (1024 * 1024), "MB ->", zpath)
