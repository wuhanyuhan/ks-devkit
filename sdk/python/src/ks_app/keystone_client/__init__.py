"""ks_app.keystone_client — 调 Keystone 控制面 API 的客户端子包。

含：
- SelfClient: 应用启动期自查托管资源凭证
- DispatcherClient: capability mesh dispatcher（v0.6.0 起）
"""
from .dispatcher_client import (
    DispatcherClient,
    InvokeAsyncResult,
    InvokeSyncResult,
    TaskSnapshot,
)
from .exceptions import KeystoneSelfFetchError
from .self_client import SelfClient

__all__ = [
    "KeystoneSelfFetchError",
    "SelfClient",
    "DispatcherClient",
    "InvokeAsyncResult",
    "InvokeSyncResult",
    "TaskSnapshot",
]
