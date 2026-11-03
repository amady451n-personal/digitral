#!/usr/bin/env python3
"""Minimal dex static-field decoder — rebuild of /tmp/dec/decode3.py (wiped).

Validated against bima+ 5.17.0 dex (PRODUCT/BIMA constants reproduced) before use.
Usage: decode_axis.py <dexdir> <out.json> [class/field-filter-regex]
Dump side-care per dex file: classesN.strings.txt (idempotent).
"""
import json
import re
import struct
import sys
from pathlib import Path


def uleb(b, o):
    r = s = 0
    while True:
        x = b[o]; o += 1
        r |= (x & 0x7F) << s; s += 7
        if not x & 0x80:
            return r, o


def dex_string(b, off):
    _n, o = uleb(b, off)
    e = b.index(b"\x00", o)
    try:
        return b[o:e].decode("utf-8")
    except UnicodeDecodeError:
        return b[o:e].decode("utf-8", "replace")


def encoded_value(b, o, strs):
    """One encoded_value -> (resolved python value, new off)."""
    h = b[o]; vt = h & 0x1F; size = (h >> 5) + 1; o += 1
    if vt == 0x30:                                   # NULL
        return None, o
    if vt == 0x1F:                                   # BOOLEAN
        return (h >> 5) == 1, o
    if vt == 0x1C:                                   # ANNOTATION
        o += 1                                       # visibility ubyte
        t = int.from_bytes(b[o:o+2], "little"); o += 2
        n, o = uleb(b, o)
        for _ in range(n):
            o += 2                                   # name_idx u16
            _v, o = encoded_value(b, o, strs)
        return None, o
    if vt == 0x1A:                                   # ARRAY
        n, o = uleb(b, o)
        for _ in range(n):
            _v, o = encoded_value(b, o, strs)
        return None, o
    raw = b[o:o + size]; o += size
    if vt == 0x17:                                   # STRING
        return strs[int.from_bytes(raw, "little")], o
    if vt in (0x00, 0x02, 0x03, 0x04, 0x06):         # BYTE/SHORT/CHAR/INT/LONG
        v = int.from_bytes(raw, "little")
        if raw and raw[-1] & 0x80:
            v -= 1 << (8 * size)
        return v, o
    if vt in (0x15, 0x13):                           # CHAR/UBYTE? plain
        return int.from_bytes(raw, "little"), o
    if vt in (0x16, 0x14):                           # FLOAT/DOUBLE variants
        return float(int.from_bytes(raw, "little")), o
    # 0x18 METHOD 0x19 METHOD_TYPE 0x1B FIELD 0x1D ENUM 0x10/0x11 floats idx —
    # treated as idx ints, resolved as numerics (fine for BuildConfig)
    return int.from_bytes(raw, "little"), o


def parse_dex(p):
    b = Path(p).read_bytes()
    assert b[:4] == b"dex\n", p
    s_sz, s_off = struct.unpack_from("<II", b, 0x38)
    t_sz, t_off = struct.unpack_from("<II", b, 0x40)
    f_sz, f_off = struct.unpack_from("<II", b, 0x50)
    c_sz, c_off = struct.unpack_from("<II", b, 0x60)

    strs = [dex_string(b, struct.unpack_from("<I", b, s_off + 4*i)[0]) for i in range(s_sz)]
    types = [struct.unpack_from("<I", b, t_off + 4*i)[0] for i in range(t_sz)]
    fields = [struct.unpack_from("<HHI", b, f_off + 8*i) for i in range(f_sz)]

    consts = {}
    skipped = 0
    for i in range(c_sz):
        base = c_off + 32*i
        class_idx = struct.unpack_from("<I", b, base)[0]
        cdo, svo = struct.unpack_from("<II", b, base + 24)
        desc = strs[types[class_idx]]
        if not cdo or not svo:
            continue
        try:
            o = cdo
            sfz, o = uleb(b, o); ifz, o = uleb(b, o); dmz, o = uleb(b, o); vmz, o = uleb(b, o)
            s = []
            fidx = 0
            for _ in range(sfz):
                diff, o = uleb(b, o); _af, o = uleb(b, o)
                fidx += diff
                s.append(fidx)
            n, o = uleb(b, svo)
            for k in range(min(n, len(s))):
                v, o = encoded_value(b, o, strs)
                fid = s[k]
                _ci, ti, ni = fields[fid]
                name = strs[ni]
                if isinstance(v, (str, int)) and v != "":
                    consts[(desc, name)] = (v, strs[types[ti]])
        except Exception:
            skipped += 1
            continue
    return consts, strs, skipped


def main():
    dexdir, outjson = Path(sys.argv[1]), sys.argv[2]
    pat = re.compile(sys.argv[3]) if len(sys.argv) > 3 else re.compile(
        r"(?i)(BuildConfig|Constant|Config|ApiUrl|Env)")
    found = {}
    for p in sorted(dexdir.glob("classes*.dex")):
        strings_file = p.with_suffix(".strings.txt")
        consts, strs, _sk = parse_dex(p)
        if not strings_file.exists():
            strings_file.write_text("\n".join(strs), encoding="utf-8")
        for (cdesc, name), (val, _t) in consts.items():
            if pat.search(cdesc) or pat.search(name):
                found[f"{cdesc}::{name}"] = val
    json.dump(found, open(outjson, "w"), indent=1, ensure_ascii=False)
    print(f"{len(found)} fields -> {outjson}")


if __name__ == "__main__":
    main()
