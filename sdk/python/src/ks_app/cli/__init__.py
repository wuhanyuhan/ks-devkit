"""ksapp CLI — Keystone MCP SDK 命令行工具。

镜像 Go sdk/go/ksapp/cli。两端 CLI 的文件格式
（mcp-config.enc / .local-dek / .mcp-key）完全互通。

入口：
  - python -m ks_app.cli
  - ksapp （pyproject [project.scripts] 注册的 ks_app.cli.__main__:main）
"""
