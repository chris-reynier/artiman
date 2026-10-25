"""Minimal Word 97-2003 (.doc) text extractor using the piece table."""
import struct
import sys
import olefile


def doc_text(path):
    ole = olefile.OleFileIO(path)
    wd = ole.openstream("WordDocument").read()
    flags = struct.unpack_from("<H", wd, 0x0A)[0]
    tbl_name = "1Table" if flags & 0x0200 else "0Table"
    tbl = ole.openstream(tbl_name).read()
    fc_clx, lcb_clx = struct.unpack_from("<II", wd, 0x01A2)
    clx = tbl[fc_clx:fc_clx + lcb_clx]
    i = 0
    while i < len(clx) and clx[i] == 0x01:
        cb = struct.unpack_from("<H", clx, i + 1)[0]
        i += 3 + cb
    if i >= len(clx) or clx[i] != 0x02:
        raise ValueError("no piece table")
    lcb = struct.unpack_from("<I", clx, i + 1)[0]
    plc = clx[i + 5:i + 5 + lcb]
    n = (lcb - 4) // 12
    cps = struct.unpack_from("<%dI" % (n + 1), plc, 0)
    out = []
    for k in range(n):
        pcd = plc[4 * (n + 1) + 8 * k:4 * (n + 1) + 8 * k + 8]
        fc = struct.unpack_from("<I", pcd, 2)[0]
        cnt = cps[k + 1] - cps[k]
        if fc & 0x40000000:
            off = (fc & 0x3FFFFFFF) // 2
            out.append(wd[off:off + cnt].decode("cp1252", "replace"))
        else:
            out.append(wd[fc:fc + 2 * cnt].decode("utf-16-le", "replace"))
    ole.close()
    return "".join(out)


if __name__ == "__main__":
    for p in sys.argv[1:]:
        t = doc_text(p)
        print("=====", p)
        print(t.replace("\r", "\n").replace("\x07", " | "))
