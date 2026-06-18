import json
from pathlib import Path

SHARED = Path(__file__).parent.parent.parent / "shared-fixtures"


def test_readiness_wire_conformance():
    """锁定 Python 侧 wire 形状与 shared-fixtures golden 一致（字段名/类型/omitempty）。"""
    fixture = json.loads((SHARED / "readiness.json").read_text(encoding="utf-8"))
    rep = fixture["readiness_report"]
    assert len(rep["gates"]) == 2

    g0 = rep["gates"][0]
    assert g0["id"] == "corpus_embed"
    assert g0["status"] == "running"
    assert g0["progress"] == 42
    assert g0["message"] == "已嵌入 1200/2900 条"

    g1 = rep["gates"][1]
    assert g1["id"] == "warm_cache"
    assert g1["status"] == "ready"
    assert "progress" not in g1  # ready 门无 progress（与 Go omitempty 对齐）
    assert "message" not in g1

    assert fixture["init_request"]["gate_id"] == "corpus_embed"
