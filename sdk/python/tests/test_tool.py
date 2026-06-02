from ks_app import tool, Param


def test_tool_decorator_attaches_metadata():
    @tool("add", "加法")
    def add(a, b):
        return a + b

    assert add._ks_tool_name == "add"
    assert add._ks_tool_description == "加法"


def test_param_defaults():
    p = Param("一个数字")
    assert p.description == "一个数字"
    assert p.default is None
    assert p.required is False


def test_param_required():
    p = Param("必填字段", required=True)
    assert p.required is True


def test_param_default_value():
    p = Param("可选字段", default=42)
    assert p.default == 42
