import pytest
from ks_app import App


def test_tool_registration():
    app = App("test")

    @app.tool("greet", "打招呼")
    async def greet(name: str = "world"):
        return {"msg": f"hello {name}"}

    assert "greet" in app._tools
    assert app._tools["greet"]["description"] == "打招呼"


def test_sync_handler_rejected():
    """同步函数注册 tool 时必须在注册阶段报错"""
    app = App("test")
    with pytest.raises(TypeError, match="必须是 async 函数"):
        @app.tool("sync_greet", "sync version")
        def sync_greet():
            return {"msg": "hello"}


def test_duplicate_tool_rejected():
    """同名 tool 重复注册应当在第二次注册时报错"""
    app = App("test")

    @app.tool("x", "first")
    async def first():
        return 1

    with pytest.raises(ValueError, match="已经注册过了"):
        @app.tool("x", "second")
        async def second():
            return 2
