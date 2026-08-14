from __future__ import annotations
import json,re
from pathlib import Path
F=Path(__file__).parent/"fixtures"/"primitives_v1.json"
ID=re.compile(r"^(?P<kind>[a-z][a-z0-9]{1,23})_(?P<body>[0-9a-f]{32})$")
def test_identifier_fixture_is_canonical_uuidv7():
    value=json.loads(F.read_text())["resource_id"];m=ID.fullmatch(value);assert m
    raw=bytes.fromhex(m.group("body"));assert raw[6]>>4==7;assert raw[8]&0xC0==0x80
    assert m.group("kind")==json.loads(F.read_text())["resource_id_kind"]
