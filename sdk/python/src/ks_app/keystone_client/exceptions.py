"""keystone_client 异常类型。"""
from __future__ import annotations


class KeystoneSelfFetchError(RuntimeError):
    """SelfClient 自取托管资源失败的通用异常。

    无论是 HTTP 错误码、网络错误、响应解析错误、业务 code != 0，全部归类到本异常，
    调用方（一般是 ks_app.App._maybe_fetch_keystone_managed_env）通过单一 except
    分支即可处理；具体原因通过 message 表达。

    对应 managed resources self-fetch 契约。
    """
