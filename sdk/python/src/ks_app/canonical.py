"""canonical 派生：与 ks-types Canonical(appID, name) 等价。

两语言 SDK 各自实现同一派生逻辑（Go 用 kstypes.Canonical，Python 一份），
靠 sdk/shared-fixtures wire-compat 锁结果一致。
"""
from __future__ import annotations


def canonical(app_id: str, name: str) -> str:
    """由 app_id + 裸名派生全局唯一 canonical_name。"""
    return f"{app_id}.{name}"
