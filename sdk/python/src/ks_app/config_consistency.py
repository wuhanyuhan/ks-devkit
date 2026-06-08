"""nav/config_mode/config_ui 组合一致性矩阵（Python 对等实现）。

权威单一事实源 = ks-types/nav_config_consistency.go::CheckNavConfigConsistency。
本模块因语言隔离是唯一第二实现，靠 sdk/shared-fixtures/nav_config_consistency.json
契约测试锁定与 Go 逐例一致；reason 文案以 Go 为权威参照。
"""
from __future__ import annotations

NAV_ABSENT = "absent"
NAV_INVALID = "invalid"
NAV_VALID = "valid"


def check_nav_config_consistency(
    nav_state: str, open_mode: str, config_mode: str, has_config_ui: bool
) -> tuple[str, bool]:
    """返回 (reason, ok)；ok=False 时 reason 是人话诊断。config_mode=="" 归一为 none。"""
    if config_mode == "":
        config_mode = "none"

    if nav_state == NAV_ABSENT:
        if config_mode in ("schema", "iframe"):
            return (
                f"声明了 config_mode={config_mode} 却未声明 nav 导航入口：配置界面将无法打开"
                "（列表显示「无入口」，强制访问报 40041）。请补 nav（schema 配置类用 open_mode=dialog）",
                False,
            )
        return ("", True)
    if nav_state == NAV_INVALID:
        return (
            "nav 声明不合法（缺 label/category/open_mode 或 open_mode 非 dialog/fullpage/tab），"
            "nav 行会被丢弃 → 应用「无入口」",
            False,
        )

    # NAV_VALID
    if open_mode == "dialog":
        if config_mode == "schema":
            return ("", True)
        if config_mode == "iframe":
            if has_config_ui:
                return ("", True)
            return (
                "open_mode=dialog + config_mode=iframe 需要 config_ui.enabled=true 且 url 非空，"
                "当前缺失 → 点击配置会报 40041",
                False,
            )
        return (
            f"open_mode=dialog + config_mode={config_mode} 无效：dialog 入口只支持 schema/iframe 配置弹窗 → 应用「无入口」",
            False,
        )
    if open_mode in ("fullpage", "tab"):
        if config_mode == "none":
            return ("", True)
        if config_mode == "schema":
            return (
                f"open_mode={open_mode} + config_mode=schema 非法：schema 配置只能在 dialog 弹窗内渲染，"
                "此 nav 会被菜单丢弃 → 应用「无入口」。配置类请改 open_mode=dialog",
                False,
            )
        if config_mode == "iframe":
            return (
                f"open_mode={open_mode} + config_mode=iframe 非法：点击会报 40041。"
                "fullpage/tab 业务前端应 config_mode=none；配置界面应 open_mode=dialog",
                False,
            )
        return (f"open_mode={open_mode} + config_mode={config_mode} 组合未知", False)
    return ("", True)  # 不可达：NAV_VALID 保证 open_mode ∈ 三枚举
