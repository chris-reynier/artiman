"""Parse raw ArtiMan Word texts into structured estimates / invoices."""
import json
import re
import sys
import unicodedata

NUM = r"-?\d[\d\s ]*(?:[.,]\d+)?"
RE_NUM = re.compile(r"^\s*(" + NUM + r")\s*€?\s*$")
RE_QTY = re.compile(r"^\s*(" + NUM + r")\s*([^\d\s][^\r]{0,12}?)?\s*$")
RE_DATE = re.compile(r"([A-Z][\w\-]+)\s+(?:le\s+)?(\d{1,2})\s*/+\s*(\d{1,2})\s*/*\s*(\d{4}|\d{2})\b")
RE_DEVIS_FN = re.compile(r"^(.*?)\s*-\s*D(\d{2})(\d{2})(\d{4})(\d{2})(\d{2})")
CIVS = ["M. et Mme", "Mr et Mme", "M et Mme", "Mme.", "Mme", "Mlle", "M.", "Mr", "Me", "Melle", "Monsieur", "Madame"]


def num(s):
    if s is None:
        return None
    m = RE_NUM.match(s.replace("\r", " ").strip())
    if not m:
        return None
    v = m.group(1).replace(" ", "").replace(" ", "").replace(",", ".")
    try:
        return float(v)
    except ValueError:
        return None


def clean(s):
    return re.sub(r"[ \t]+", " ", s.replace("\r", "\n").replace("\x0b", "\n")).strip(" \n\t")


def norm_key(s):
    s = unicodedata.normalize("NFKD", s).encode("ascii", "ignore").decode()
    s = re.sub(r"[^A-Za-z0-9 ]", " ", s).upper()
    return re.sub(r"\s+", " ", s).strip()


def split_civ(name):
    for c in CIVS:
        if name.startswith(c + " ") or name == c:
            return c.rstrip("."), name[len(c):].strip()
    return "", name


def parse_doc(text):
    # Multi-page documents repeat the logo/header table on each page
    d = {"pages": max(1, text.count("\x01\x07"))}
    m = RE_DATE.search(text)
    if m:
        y = int(m.group(4))
        y = y + 2000 if y < 100 else y
        d["date"] = "%04d-%02d-%02d" % (y, int(m.group(3)), int(m.group(2)))
        d["place"] = m.group(1)
    m = re.search(r"(?i)Facture\s*n?\S?\s*([0-9][^\r\x07]*)", text)
    if "Devis Estimatif" in text[:2000] and not (m and m.start() < text.find("Devis Estimatif")):
        d["kind"] = "devis"
    elif m:
        d["kind"] = "facture"
        d["number_raw"] = m.group(1).strip()
    else:
        d["kind"] = "inconnu"
    # Client block: cell after the phone numbers, lines starting with a tab
    cells = text.split("\x07")
    for c in cells[:12]:
        if "\t" in c and "Tel." not in c and "Siret" not in c:
            lines = [ln.strip() for ln in c.split("\r") if ln.strip()]
            if lines:
                civ, name = split_civ(lines[0])
                d["client"] = {"civility": civ, "name": name, "address": lines[1:]}
                cp = [ln for ln in lines[1:] if re.match(r"^\d{2}\s?\d{3}\b", ln)]
                if cp:
                    mm = re.match(r"^(\d{2})\s?(\d{3})\s*(.*)$", cp[-1])
                    d["client"]["zip"] = mm.group(1) + mm.group(2)
                    d["client"]["city"] = mm.group(3).strip()
                break
    m = re.search(r"Date de validit\S* : (\d{1,2})/(\d{1,2})/(\d{4})", text)
    if m:
        d["valid_until"] = "%s-%02d-%02d" % (m.group(3), int(m.group(2)), int(m.group(1)))
    # Items table
    items, sections = [], []
    section = ""
    for page in text.split("\x01\x07"):
        h = page.find("TOTAL HT\x07\x07")
        if h < 0:
            continue
        body = page[h + len("TOTAL HT\x07\x07"):]
        bc = body.split("\x07")
        end = len(bc)
        for i, c in enumerate(bc):
            if "Date de validit" in c or re.match(r"^\s*Total HT\s*\r", c) or "Bon pour accord" in c:
                end = i
                break
        bc = bc[:end]
        last_item = None
        i = 0
        while i < len(bc):
            g = bc[i:i + 5] + [""] * (5 - len(bc[i:i + 5]))
            c0 = clean(g[0])
            q, pu, tot = g[1].strip(), num(g[2]), num(g[3])
            mq = RE_QTY.match(q.replace("\r", " ")) if q else None
            if tot is not None and (mq or (not q and pu is None) or pu is not None):
                qty = float(mq.group(1).replace(" ", "").replace(",", ".")) if mq else None
                unit = (mq.group(2) or "").strip() if mq else ""
                if (qty == 0 or qty is None) and not tot and not pu:
                    if c0:
                        section = c0
                        sections.append(section)
                    last_item = None
                else:
                    last_item = {"section": section, "label": c0, "qty": qty, "unit": unit,
                                 "pu": pu, "total": tot, "desc": ""}
                    items.append(last_item)
                i += 5
                continue
            txt = clean(" ".join(x for x in g[:4]))
            if txt:
                if last_item is not None and not last_item["desc"]:
                    last_item["desc"] = txt
                else:
                    # A text-only row that is not the item's description is a section heading
                    last_item = None
                    section = txt
                    sections.append(section)
            else:
                last_item = None if last_item and last_item["desc"] else last_item
            i += 5
    d["items"] = items
    # Totals block
    for i, c in enumerate(cells):
        if (re.search(r"(^|\r)\s*Total HT\s*\r", c) and "TVA" in c
                or re.match(r"^\s*Net . payer\s*(\r|$)", c)) and i + 1 < len(cells):
            labels = [x.strip() for x in c.split("\r")]
            vals = [x.strip() for x in cells[i + 1].split("\r")]
            labels = labels[labels.index("Total HT"):] if "Total HT" in labels else labels
            tot = {}
            for lab, val in zip([x for x in labels if x], [x for x in vals if x]):
                v = num(val)
                if lab.startswith("Total HT"):
                    tot["ht"] = v
                elif lab.startswith("TVA"):
                    tot["tva"] = v
                    mr = re.search(r"([\d.,]+)", lab)
                    if mr:
                        tot["tva_rate"] = float(mr.group(1).rstrip(".").replace(",", "."))
                elif lab.startswith("Total TTC"):
                    tot["ttc"] = v
                elif lab.startswith("Acompte") or lab.startswith("Accompte"):
                    tot["acompte"] = v
                elif lab.startswith("Net"):
                    tot["net"] = v
                elif lab.startswith("Remise"):
                    tot["remise"] = v
                else:
                    tot.setdefault("other", []).append([lab, v])
            if "ht" not in tot and tot.get("net") is not None:
                tot.update(ht=tot["net"], tva=0.0, tva_rate=0.0, ttc=tot["net"])
            d["totals"] = tot
            break
    if "auto liquidation" in text or "autoliquidation" in text:
        d["reverse_charge"] = True
    if "totals" not in d and items:
        ht = round(sum(it["total"] or 0 for it in items), 2)
        d["totals"] = {"ht": ht, "recomputed": True}
    if "signature du devis" in text:
        d["terms"] = "30% à la signature, 30% au début des travaux"
    elif "ception de la facture" in text:
        d["terms"] = "Règlement à réception"
    m = re.search(r"Siret n\S*\s*([\d ]+)", text)
    if m:
        d["siret"] = m.group(1).strip()
    return d


def main(src, dst):
    rows = json.load(open(src, encoding="utf-8"))
    out = []
    for r in rows:
        if "text" not in r:
            continue
        d = parse_doc(r["text"])
        d.update(folder=r["folder"], file=r["file"], sha1=r["sha1"], size=r["size"])
        fn = r["file"].rsplit(".", 1)[0]
        m = RE_DEVIS_FN.match(fn)
        if m:
            d["fn_client"] = m.group(1).strip()
            d["fn_datetime"] = "%s-%s-%sT%s:%s" % (m.group(4), m.group(3), m.group(2), m.group(5), m.group(6))
        else:
            mm = re.match(r"^\s*F\s*([\d]+)\s*[._\-/]?\s*(\d{2})?\s*[-–]?\s*(.*)$", fn)
            if mm:
                d["fn_number"] = mm.group(1) + ("-" + mm.group(2) if mm.group(2) else "")
                d["fn_client"] = mm.group(3).strip(" -")
            elif " - " in fn:
                a, b = fn.split(" - ", 1)
                d["fn_client"] = a.strip()
        out.append(d)
    json.dump(out, open(dst, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(len(out), "documents parsed")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
