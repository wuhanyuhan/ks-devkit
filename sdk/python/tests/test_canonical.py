from ks_app.canonical import canonical


def test_canonical_derives_app_id_dot_name():
    assert canonical("ks-mcp-x", "web_search") == "ks-mcp-x.web_search"
