"""Consolidate parsed documents into clients / documents / links for the HTML report."""
import json
import re
import sys
from collections import defaultdict
from datetime import date

sys.path.insert(0, __import__("os").path.dirname(__import__("os").path.abspath(__file__)))
from parse import norm_key, split_civ  # noqa: E402

LEGAL_WORDS = {"M", "MME", "MLLE", "MR", "ET"}


def client_key(name):
    k = norm_key(name)
    return " ".join(w for w in k.split() if w not in LEGAL_WORDS) or k


def d2o(s):
    y, m, d = (int(x) for x in s.split("-"))
    try:
        return date(y, m, d).toordinal()
    except ValueError:
        return date(y, m, 1).toordinal()


def main(src, dst):
    docs = [d for d in json.load(open(src, encoding="utf-8"))
            if not d["file"].lower().endswith(".tmp")]
    # Exact duplicates (same bytes): keep the first, record the copies
    seen = {}
    kept = []
    for d in sorted(docs, key=lambda x: (len(x["file"]), x["folder"], x["file"])):
        if d["sha1"] in seen:
            seen[d["sha1"]].setdefault("copies", []).append(d["folder"] + "/" + d["file"])
            continue
        seen[d["sha1"]] = d
        kept.append(d)
    docs = kept

    clients = {}
    out_docs = []
    for i, d in enumerate(sorted(docs, key=lambda x: (x.get("date") or "9999", x["file"]))):
        c = d.get("client") or {}
        name = c.get("name") or d.get("fn_client") or "(inconnu)"
        if not name.strip():
            name = d.get("fn_client") or "(inconnu)"
        key = client_key(name)
        if not key:
            key = client_key(d.get("fn_client") or "") or "INCONNU"
        cl = clients.setdefault(key, {"key": key, "names": defaultdict(int), "addresses": {}, "docs": []})
        cl["names"][name.strip()] += 1
        if c.get("address"):
            cl["addresses"][" / ".join(c["address"])] = d.get("date") or ""
        if c.get("civility"):
            cl["civility"] = c["civility"]
        if c.get("city"):
            cl["city"], cl["zip"] = c["city"], c.get("zip", "")
        t = d.get("totals") or {}
        flags = []
        if t.get("recomputed"):
            flags.append("Totaux absents du document : HT recalculé depuis les lignes")
        s = round(sum(it["total"] or 0 for it in d["items"]), 2)
        if t.get("ht") is not None and d["items"] and abs(s - t["ht"]) > 1.01 and not t.get("recomputed"):
            flags.append("Somme des lignes (%.2f) différente du total HT" % s)
        if d.get("fn_client") and client_key(d["fn_client"]) and client_key(d["fn_client"]) != key \
                and not key.startswith(client_key(d["fn_client"])) and not client_key(d["fn_client"]).startswith(key):
            flags.append("Nom de fichier (%s) différent du client dans le document" % d["fn_client"])
        if d["kind"] == "devis" and d.get("fn_datetime") and d.get("date") and d["fn_datetime"][:10] != d["date"]:
            flags.append("Date du document différente de la date du fichier (%s)" % d["fn_datetime"][:10])
        if d["kind"] == "facture" and "Devis" in d["folder"]:
            flags.append("Facture rangée dans le dossier Devis")
        if d.get("reverse_charge"):
            flags.append("Autoliquidation de TVA (sous-traitance)")
        if d["kind"] == "inconnu":
            flags.append("Type de document non reconnu")
        title = next((x for x in [d["items"][0]["section"] if d["items"] else "",
                                  d["items"][0]["label"] if d["items"] else ""] if x), "")
        od = {
            "id": i, "kind": d["kind"], "date": d.get("date"), "client": key,
            "number": d.get("number_raw") or d.get("fn_number") or "",
            "title": title, "place": d.get("place"),
            "ht": t.get("ht"), "tva_rate": t.get("tva_rate"), "tva": t.get("tva"),
            "ttc": t.get("ttc"), "acompte": t.get("acompte"), "net": t.get("net"),
            "remise": t.get("remise"), "valid_until": d.get("valid_until"),
            "terms": d.get("terms"), "pages": d.get("pages"),
            "items": [{k: it[k] for k in ("section", "label", "qty", "unit", "pu", "total", "desc")} for it in d["items"]],
            "path": (d["folder"].replace("\\", "/") + "/" + d["file"]).lstrip("./"),
            "fn_datetime": d.get("fn_datetime"), "fn_number": d.get("fn_number"),
            "civility_raw": c.get("civility", ""), "zip": c.get("zip", ""), "city": c.get("city", ""),
            "copies": d.get("copies", []), "flags": flags,
            "address": c.get("address", []),
        }
        out_docs.append(od)
        cl["docs"].append(i)

    # Merge "NAME" into "NAME FIRSTNAME" when they live in the same city (or city unknown)
    for short in sorted(clients, key=len):
        longer = [k for k in clients if k != short and k.startswith(short + " ")]
        if len(longer) != 1:
            continue
        a, b = clients[short], clients[longer[0]]
        if a.get("city") and b.get("city") and norm_key(a["city"]) != norm_key(b["city"]):
            continue
        for n, v in a["names"].items():
            b["names"][n] += v
        b["addresses"].update(a["addresses"])
        b["docs"].extend(a["docs"])
        for i in a["docs"]:
            out_docs[i]["client"] = longer[0]
        del clients[short]

    # Link invoices to estimates: same client, estimate not after invoice, closest HT
    by_client = defaultdict(list)
    for od in out_docs:
        by_client[od["client"]].append(od)
    used = set()
    stats = defaultdict(int)
    for key, lst in by_client.items():
        devis = [x for x in lst if x["kind"] == "devis" and x["date"]]
        for f in sorted([x for x in lst if x["kind"] == "facture"], key=lambda x: x["date"] or ""):
            best, best_score = None, None
            for dv in devis:
                if dv["id"] in used or not f["date"]:
                    continue
                dd = d2o(f["date"]) - d2o(dv["date"])
                if dd < -31:
                    continue
                same = f["ht"] is not None and dv["ht"] is not None and abs(f["ht"] - dv["ht"]) < 0.5
                labels_f = {(it["label"], it["total"]) for it in f["items"]}
                labels_d = {(it["label"], it["total"]) for it in dv["items"]}
                overlap = len(labels_f & labels_d) / max(1, len(labels_f))
                if not same and overlap < 0.5:
                    continue
                score = (0 if same else 1, -overlap, abs(dd))
                if best_score is None or score < best_score:
                    best, best_score = dv, score
            if best:
                used.add(best["id"])
                f["from_devis"] = best["id"]
                best["to_facture"] = f["id"]
                stats["exact" if best_score[0] == 0 else "partial"] += 1
                f["link_quality"] = "exact" if best_score[0] == 0 else "partiel"
            else:
                stats["none"] += 1

    out_clients = []
    for key, cl in clients.items():
        names = sorted(cl["names"].items(), key=lambda x: -x[1])
        addr = sorted(cl["addresses"].items(), key=lambda x: x[1])
        out_clients.append({
            "key": key, "name": names[0][0], "aliases": [n for n, _ in names[1:]],
            "civility": cl.get("civility", ""), "city": cl.get("city", ""), "zip": cl.get("zip", ""),
            "address": addr[-1][0] if addr else "", "docs": cl["docs"],
        })
    out_clients.sort(key=lambda c: c["name"].upper())
    json.dump({"generated": date.today().isoformat(), "clients": out_clients, "docs": out_docs},
              open(dst, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
    print("docs", len(out_docs), "clients", len(out_clients), dict(stats))
    print("flags", sum(1 for d in out_docs if d["flags"]))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
