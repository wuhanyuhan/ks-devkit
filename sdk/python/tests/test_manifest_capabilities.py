"""manifest provides/requires capabilities 解析单测（对齐 ks-types v0.19.0）。"""
import textwrap
from pathlib import Path

import pytest

from ks_app.manifest import (
    BackendSpec,
    ProvidesCapability,
    RequiresCapability,
    load_manifest_capabilities,
    load_manifest_requires,
)


def _write_manifest(tmp_path: Path, body: str) -> str:
    p = tmp_path / "manifest.yaml"
    p.write_text(textwrap.dedent(body), encoding="utf-8")
    return str(p)


def test_empty_manifest_returns_empty_list(tmp_path):
    path = _write_manifest(tmp_path, "name: foo\n")
    assert load_manifest_capabilities(path) == []
    assert load_manifest_requires(path) == []


def test_missing_manifest_returns_empty_list(tmp_path):
    assert load_manifest_capabilities(str(tmp_path / "nope.yaml")) == []
    assert load_manifest_requires(str(tmp_path / "nope.yaml")) == []


def test_parse_provides_capabilities_mcp_tool_backend(tmp_path):
    path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: create_article
              execution_mode: long_running
              timeout_ms: 300000
              backend:
                kind: mcp_tool
                tool_name: create_article
            - name: list_articles
              execution_mode: sync
              backend:
                kind: mcp_tool
                tool_name: list_articles
    """)
    caps = load_manifest_capabilities(path)
    assert len(caps) == 2
    assert caps[0].name == "create_article"
    assert caps[0].execution_mode == "long_running"
    assert caps[0].timeout_ms == 300000
    assert caps[0].backend.kind == "mcp_tool"
    assert caps[0].backend.tool_name == "create_article"
    assert caps[1].name == "list_articles"
    assert caps[1].execution_mode == "sync"


def test_parse_provides_http_endpoint_backend(tmp_path):
    path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: generate
              execution_mode: long_running
              backend:
                kind: http_endpoint
                path: /api/generate
                method: POST
    """)
    caps = load_manifest_capabilities(path)
    assert len(caps) == 1
    assert caps[0].backend.kind == "http_endpoint"
    assert caps[0].backend.path == "/api/generate"
    assert caps[0].backend.method == "POST"


def test_parse_requires_capabilities(tmp_path):
    path = _write_manifest(tmp_path, """
        requires:
          capabilities:
            - canonical_name: ks-mcp-image-gen.generate
              mode: required
              reason: 写文章需要配封面
            - canonical_name: ks-mcp-summary.summarize
              mode: optional
    """)
    reqs = load_manifest_requires(path)
    assert len(reqs) == 2
    assert reqs[0].canonical_name == "ks-mcp-image-gen.generate"
    assert reqs[0].mode == "required"
    assert reqs[0].reason == "写文章需要配封面"
    assert reqs[1].mode == "optional"


def test_unknown_backend_kind_kept_verbatim(tmp_path):
    """SDK 解析不强校验 backend.kind；future-proof。"""
    path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: bar
              execution_mode: sync
              backend:
                kind: future_kind
    """)
    caps = load_manifest_capabilities(path)
    assert caps[0].backend.kind == "future_kind"


def test_corrupted_yaml_returns_empty(tmp_path):
    """yaml 解析失败时返空列表（与原 _read_manifest 兼容行为对齐）。"""
    path = _write_manifest(tmp_path, "{[ not valid yaml")
    assert load_manifest_capabilities(path) == []
    assert load_manifest_requires(path) == []
