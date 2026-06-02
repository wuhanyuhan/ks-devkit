"""ks_app Python SDK 的 conformance claimant。

它声称遵守 ks-devkit/conformance/auth/ v1.0.0 契约。
除 echo 工具外不做任何业务，行为被 conformance 测试冻结。

不要修改 echo 的名字、schema 或返回值——否则 conformance case 16/17 会失败。
"""
from ks_app import App

app = App(
    "conformance-claimant",
    keystone_auth=True,
    version="conformance-v1.0.0",
)


@app.tool(
    name="echo",
    description="Echo message as-is (conformance test tool)",
    input_schema={
        "type": "object",
        "properties": {"message": {"type": "string"}},
    },
)
async def echo(message: str = "") -> dict:
    return {"echoed": message}


if __name__ == "__main__":
    app.run()
