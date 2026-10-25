"""Rebuild an ArtiManD.db file from the history extracted out of the Word documents.

File format (reverse-engineered from ArtiManD.exe samples, all integers little-endian):

  header   "DTB@" 00 "CDB0000"  u32 crc32(file[16:])  u16 next_invoice_number  u32 customer_count
  customer "DCRC0000" "TSUC0000" u32 civility (0 M., 1 Mme, 2 M. et Mme, 3 Mlle, 4 autre)
           9 x cp1252 zero-terminated strings: name, first name, address, zip, city, phone, mobile, fax, email
           u32 estimate_count, estimates...
  estimate "MTSE0000" u16 invoice_number  u8 invoiced  f32 discount_ratio  f32 advance
           "EDTM" "DATE" u8 day u8 month u16 year  "TIME" u8 hour u8 minute u8 second
           u32 task_count, tasks...
  task     "KROW0000" title\\0 description\\0 unit\\0  f32 unit_price  f32 quantity  f32 vat_ratio
"""
import json
import re
import struct
import sys
import zlib

CIV = {"M": 0, "Mr": 0, "Monsieur": 0, "Mme": 1, "Madame": 1, "M. et Mme": 2, "Mr et Mme": 2,
       "M et Mme": 2, "Mlle": 3, "Melle": 3}


def cstr(s):
    return (s or "").replace("\n", "\r\n").encode("cp1252", "replace") + b"\x00"


def f32(v):
    return struct.pack("<f", float(v or 0))


def split_name(full):
    """'DUPONT Jean François' -> ('DUPONT', 'Jean François'); all-caps names stay whole."""
    words = full.split()
    i = 0
    while i < len(words) and words[i].upper() == words[i] and re.search(r"[A-Z]", words[i]):
        i += 1
    if i == 0 or i == len(words):
        return full.strip(), ""
    return " ".join(words[:i]), " ".join(words[i:])


def fmt_zip(z):
    return z[:2] + " " + z[2:] if len(z) == 5 else z


def task(title, desc, unit, pu, qty, vat):
    return b"KROW0000" + cstr(title) + cstr(desc) + cstr(unit) + f32(pu) + f32(qty) + f32(vat)


def tasks_of(doc):
    vat = (doc.get("tva_rate") or 0) / 100.0
    out, section = [], None
    for it in doc["items"]:
        if it["section"] and it["section"] != section:
            section = it["section"]
            out.append(task(section, "", "U", 0, 0, vat))
        qty, pu, total = it["qty"], it["pu"], it["total"] or 0
        if qty is None or qty == 0:
            qty, pu = 1, total
        elif pu is None:
            pu = total / qty
        out.append(task(it["label"], it["desc"], it["unit"] or "U", pu, qty, vat))
    if not out and doc.get("ht"):
        out.append(task(doc.get("title") or "Montant global", "", "U", doc["ht"], 1, vat))
    return out


def invoice_number(doc):
    m = re.match(r"\s*0*(\d+)", doc.get("number") or "")
    if m and int(m.group(1)) < 65536:
        return int(m.group(1))
    m = re.match(r"0*(\d+)", doc.get("fn_number") or "")
    return int(m.group(1)) if m else 0


def estimate(doc, invoice=None):
    src = invoice or doc
    date = doc.get("date") or src.get("date") or "2000-01-01"
    y, mo, d = (int(x) for x in date.split("-"))
    hh = mi = 0
    if doc.get("fn_datetime"):
        hh, mi = int(doc["fn_datetime"][11:13]), int(doc["fn_datetime"][14:16])
    ht = src.get("ht") or 0
    discount = (src.get("remise") or 0) / ht if src.get("remise") and ht else 0
    ts = tasks_of(src)
    return (b"MTSE0000" + struct.pack("<HB", invoice_number(invoice) if invoice else 0, 1 if invoice else 0)
            + f32(discount) + f32(src.get("acompte") or 0)
            + b"EDTM" + b"DATE" + struct.pack("<BBH", d, mo, y) + b"TIME" + struct.pack("<BBB", hh, mi, 0)
            + struct.pack("<I", len(ts)) + b"".join(ts))


def main(src, dst, next_invoice):
    data = json.load(open(src, encoding="utf-8"))
    docs = data["docs"]
    body = b""
    n_est = 0
    for c in data["clients"]:
        cdocs = sorted((docs[i] for i in c["docs"]), key=lambda d: d.get("date") or "")
        if not cdocs:
            continue
        last = cdocs[-1]
        name, first = split_name(c["name"])
        civ = CIV.get(c.get("civility") or "", 4)
        addr_lines = [ln for ln in last.get("address", []) if not re.match(r"^\d{2}\s?\d{3}\b", ln)]
        recs = []
        for d in cdocs:
            if d["kind"] == "devis" and d.get("to_facture") is None:
                recs.append(estimate(d))
            elif d["kind"] == "facture":
                dv = docs[d["from_devis"]] if d.get("from_devis") is not None else d
                recs.append(estimate(dv, invoice=d))
        n_est += len(recs)
        body += (b"DCRC0000TSUC0000" + struct.pack("<I", civ)
                 + cstr(name) + cstr(first) + cstr(" ".join(addr_lines)) + cstr(fmt_zip(c.get("zip") or ""))
                 + cstr(c.get("city") or "") + cstr("") + cstr("") + cstr("") + cstr("")
                 + struct.pack("<I", len(recs)) + b"".join(recs))
    payload = struct.pack("<HI", next_invoice, len(data["clients"])) + body
    out = b"DTB@\x00CDB0000" + struct.pack("<I", zlib.crc32(payload) & 0xFFFFFFFF) + payload
    open(dst, "wb").write(out)
    print("customers", len(data["clients"]), "records", n_est, "bytes", len(out), "next invoice", next_invoice)


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2], int(sys.argv[3]) if len(sys.argv) > 3 else 541)
