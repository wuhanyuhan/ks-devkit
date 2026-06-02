import json
from pathlib import Path

from ks_app.canonical import canonical

SHARED = Path(__file__).parent.parent.parent / "shared-fixtures"


def test_canonical_derivation_conformance():
    fixture = json.loads((SHARED / "canonical_derivation.json").read_text())
    for c in fixture["cases"]:
        assert canonical(c["app_id"], c["name"]) == c["canonical"]
