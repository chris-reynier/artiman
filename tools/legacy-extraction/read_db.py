"""Decode an ArtiManD.db file (format documented in gen_db.py) to JSON."""
import json
import struct
import sys
import zlib


class Reader:
    def __init__(self, b):
        self.b, self.i = b, 0

    def tag(self, t):
        got = self.b[self.i:self.i + len(t)]
        if got != t:
            raise ValueError("expected %r at %d, got %r" % (t, self.i, got))
        self.i += len(t)

    def u(self, fmt):
        v = struct.unpack_from("<" + fmt, self.b, self.i)
        self.i += struct.calcsize("<" + fmt)
        return v if len(v) > 1 else v[0]

    def s(self):
        j = self.b.index(b"\x00", self.i)
        v = self.b[self.i:j].decode("cp1252")
        self.i = j + 1
        return v


def read(path):
    b = open(path, "rb").read()
    r = Reader(b)
    r.tag(b"DTB@\x00CDB0000")
    crc = r.u("I")
    ok = crc == zlib.crc32(b[16:]) & 0xFFFFFFFF
    nxt, n = r.u("HI")
    customers = []
    for _ in range(n):
        r.tag(b"DCRC0000TSUC0000")
        c = {"civility": r.u("I")}
        for k in ("name", "first_name", "address", "zip", "city", "phone", "mobile", "fax", "email"):
            c[k] = r.s()
        c["estimates"] = []
        for _ in range(r.u("I")):
            r.tag(b"MTSE0000")
            e = {"invoice_number": r.u("H"), "invoiced": r.u("B"), "discount": round(r.u("f"), 4), "advance": round(r.u("f"), 2)}
            r.tag(b"EDTM")
            r.tag(b"DATE")
            d, m, y = r.u("BBH")
            r.tag(b"TIME")
            hh, mi, ss = r.u("BBB")
            e["datetime"] = "%04d-%02d-%02d %02d:%02d:%02d" % (y, m, d, hh, mi, ss)
            e["tasks"] = []
            for _ in range(r.u("I")):
                r.tag(b"KROW0000")
                t = {"title": r.s(), "description": r.s(), "unit": r.s()}
                t["unit_price"], t["quantity"], t["vat"] = (round(x, 4) for x in r.u("fff"))
                e["tasks"].append(t)
            c["estimates"].append(e)
        customers.append(c)
    if r.i != len(b):
        raise ValueError("trailing bytes at %d" % r.i)
    return {"crc_ok": ok, "next_invoice": nxt, "customers": customers}


if __name__ == "__main__":
    out = read(sys.argv[1])
    if len(sys.argv) > 2:
        json.dump(out, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    n_e = sum(len(c["estimates"]) for c in out["customers"])
    print("crc_ok", out["crc_ok"], "next_invoice", out["next_invoice"], "customers", len(out["customers"]), "estimates", n_e,
          "invoiced", sum(e["invoiced"] for c in out["customers"] for e in c["estimates"]))
