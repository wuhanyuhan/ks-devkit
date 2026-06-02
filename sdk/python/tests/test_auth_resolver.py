"""auth_resolver 三层优先级 + strict-by-default + insecure 逃生测试。"""
import pytest

from ks_app.auth_resolver import AuthResolveError, resolve_auth


def test_default_returns_none():
    effective, url = resolve_auth(code_mode=None, manifest_path="nonexistent.yaml", env={})
    assert effective == "none"
    assert url == ""


def test_code_option_keystone_jwks_reads_env():
    effective, url = resolve_auth(
        code_mode="keystone_jwks",
        manifest_path="nonexistent.yaml",
        env={"KEYSTONE_JWKS_URL": "http://example.com/jwks"},
    )
    assert effective == "keystone_jwks"
    assert url == "http://example.com/jwks"


def test_insecure_escape_overrides():
    effective, url = resolve_auth(
        code_mode="keystone_jwks",
        manifest_path="nonexistent.yaml",
        env={"KS_APP_AUTH_MODE": "insecure", "KEYSTONE_JWKS_URL": "http://x"},
    )
    assert effective == "none"
    assert url == ""


def test_strict_by_default_without_url_raises():
    with pytest.raises(AuthResolveError) as ei:
        resolve_auth(code_mode="keystone_jwks", manifest_path="nonexistent.yaml", env={})
    assert "KEYSTONE_JWKS_URL" in str(ei.value)


def test_manifest_fallback(tmp_path):
    f = tmp_path / "manifest.yaml"
    f.write_text("""
id: t
name: t
version: "1.0"
type: service
mount:
  service:
    auth_mode: keystone_jwks
""")
    effective, url = resolve_auth(
        code_mode=None,
        manifest_path=str(f),
        env={"KEYSTONE_JWKS_URL": "http://x"},
    )
    assert effective == "keystone_jwks"
    assert url == "http://x"


def test_manifest_extension_fallback(tmp_path):
    f = tmp_path / "manifest.yaml"
    f.write_text("""
id: t
name: t
version: "1.0"
type: extension
mount:
  extension:
    mcp_server_name: t
    auth_mode: keystone_jwks
""")
    effective, url = resolve_auth(
        code_mode=None,
        manifest_path=str(f),
        env={"KEYSTONE_JWKS_URL": "http://x"},
    )
    assert effective == "keystone_jwks"


def test_code_option_overrides_manifest(tmp_path):
    """代码 Option keystone_jwks 应优先于 manifest 的 none。"""
    f = tmp_path / "manifest.yaml"
    f.write_text("""
id: t
name: t
type: service
mount:
  service:
    auth_mode: none
""")
    effective, _ = resolve_auth(
        code_mode="keystone_jwks",
        manifest_path=str(f),
        env={"KEYSTONE_JWKS_URL": "http://x"},
    )
    assert effective == "keystone_jwks"
