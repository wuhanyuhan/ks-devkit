"""manifest.yaml 解析测试。"""
import os
import tempfile
import textwrap

from ks_app.manifest import (
    load_manifest_auth_mode,
    load_manifest_capabilities,
    load_manifest_config_ui,
)


def test_load_manifest_service_auth_mode(tmp_path):
    f = tmp_path / "manifest.yaml"
    f.write_text("""
id: test
name: test
version: "1.0"
type: service
mount:
  service:
    auth_mode: keystone_jwks
""")
    assert load_manifest_auth_mode(str(f)) == "keystone_jwks"


def test_load_manifest_extension_auth_mode(tmp_path):
    f = tmp_path / "manifest.yaml"
    f.write_text("""
id: test
name: test
version: "1.0"
type: extension
mount:
  extension:
    mcp_server_name: test
    auth_mode: keystone_jwks
""")
    assert load_manifest_auth_mode(str(f)) == "keystone_jwks"


def test_load_manifest_missing_returns_none(tmp_path):
    assert load_manifest_auth_mode(str(tmp_path / "nonexistent.yaml")) is None


def test_load_manifest_no_auth_mode(tmp_path):
    f = tmp_path / "manifest.yaml"
    f.write_text("""
id: test
name: test
version: "1.0"
type: service
mount:
  service: {}
""")
    assert load_manifest_auth_mode(str(f)) in (None, "none", "")


def test_load_manifest_config_ui(tmp_path):
    f = tmp_path / "manifest.yaml"
    f.write_text("""
id: test
name: test
version: "1.0"
type: service
mount:
  service:
    config_ui:
      path: /config-ui/
""")
    result = load_manifest_config_ui(str(f))
    assert result == {"path": "/config-ui/"}


def test_load_manifest_config_ui_missing(tmp_path):
    assert load_manifest_config_ui(str(tmp_path / "nope.yaml")) is None


def test_load_manifest_corrupt_yaml_ignored(tmp_path):
    f = tmp_path / "manifest.yaml"
    f.write_text("not: [valid yaml")
    assert load_manifest_auth_mode(str(f)) is None


def _write_manifest(body: str) -> str:
    fd, path = tempfile.mkstemp(suffix=".yaml")
    with os.fdopen(fd, "w") as f:
        f.write(textwrap.dedent(body))
    return path


def test_provides_capability_parses_bare_name_and_input_schema():
    path = _write_manifest("""
        id: ks-mcp-x
        provides:
          capabilities:
            - name: web_search
              execution_mode: sync
              backend:
                kind: mcp_tool
                tool_name: web_search
              input_schema:
                type: object
                properties:
                  q: {type: string}
    """)
    caps = load_manifest_capabilities(path)
    os.remove(path)
    assert len(caps) == 1
    assert caps[0].name == "web_search"
    assert caps[0].input_schema == {
        "type": "object", "properties": {"q": {"type": "string"}},
    }
