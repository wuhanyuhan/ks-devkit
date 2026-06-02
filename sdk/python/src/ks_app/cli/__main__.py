"""ksapp CLI argparse 入口（镜像 Go SDK CLI）。

用法：
  python -m ks_app.cli config set --key=<k> --value=<v>
  python -m ks_app.cli config set --file=<path>
  python -m ks_app.cli config show
  python -m ks_app.cli config reset
  python -m ks_app.cli pubkey
  python -m ks_app.cli pubkey rotate [--print-only]
  python -m ks_app.cli pubkey prune-old

退出码：
  0 — 成功
  1 — 业务错误（exit_err 打 stderr 后退出）
  2 — argparse 缺 flag / 未知子命令（argparse 默认行为）
"""
from __future__ import annotations

import argparse
import sys

from .config_cmds import cmd_config_reset, cmd_config_set, cmd_config_show
from .fetch_env_cmds import cmd_fetch_env
from .pubkey_cmds import cmd_pubkey_prune_old, cmd_pubkey_rotate, cmd_pubkey_show


def build_parser() -> argparse.ArgumentParser:
    """构建 ksapp 的 argparse 解析器（三级子命令）。"""
    parser = argparse.ArgumentParser(
        prog="ksapp",
        description="ksapp — Keystone MCP SDK CLI",
    )
    sub = parser.add_subparsers(dest="cmd", required=True)

    # ---- config ----
    p_config = sub.add_parser("config", help="配置管理")
    c_sub = p_config.add_subparsers(dest="subcmd", required=True)

    p_set = c_sub.add_parser("set", help="设置配置（--key/--value 或 --file）")
    p_set.add_argument("--key", help="单字段 key（snake_case）")
    p_set.add_argument("--value", help="单字段 value")
    p_set.add_argument("--file", help="YAML / JSON 配置文件路径（批量）")
    p_set.set_defaults(func=cmd_config_set)

    c_sub.add_parser("show", help="显示当前配置（敏感字段脱敏）").set_defaults(
        func=cmd_config_show
    )
    c_sub.add_parser("reset", help="清空配置（回到 unconfigured）").set_defaults(
        func=cmd_config_reset
    )

    # ---- pubkey ----
    p_pub = sub.add_parser("pubkey", help="公钥管理")
    # 先给父解析器挂默认 handler（无子命令时 → show），再开 subparsers
    p_pub.set_defaults(func=cmd_pubkey_show)
    p_sub = p_pub.add_subparsers(dest="subcmd")

    p_rotate = p_sub.add_parser("rotate", help="密钥轮换")
    p_rotate.add_argument(
        "--print-only",
        action="store_true",
        dest="print_only",
        help="env/Secret 模式推荐；只生成不写文件",
    )
    p_rotate.set_defaults(func=cmd_pubkey_rotate)

    p_sub.add_parser(
        "prune-old", help="7 天过渡期结束后清除 .mcp-key.old"
    ).set_defaults(func=cmd_pubkey_prune_old)

    # ---- fetch-env（应用自查托管资源凭证）----
    p_fetch = sub.add_parser(
        "fetch-env",
        help="向 keystone 拉取本应用被分配的托管资源凭证（dotenv/json/shell）",
    )
    p_fetch.add_argument("--gateway", required=True, help="Keystone 网关地址（如 http://localhost:5188）")
    p_fetch.add_argument("--token", required=True, help="KS_APP_TOKEN 凭证")
    p_fetch.add_argument(
        "--format",
        choices=["dotenv", "json", "shell"],
        default="dotenv",
        help="输出格式（默认 dotenv）",
    )
    p_fetch.set_defaults(func=cmd_fetch_env)

    return parser


def main(argv: list[str] | None = None) -> None:
    """CLI 入口。argv 为 None 时取 sys.argv[1:]。"""
    parser = build_parser()
    args = parser.parse_args(argv)
    args.func(args)


if __name__ == "__main__":
    main()
    sys.exit(0)
