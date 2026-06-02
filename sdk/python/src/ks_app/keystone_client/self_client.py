"""SelfClient：应用启动时调 keystone `GET /v1/apps/self/resources` 拉自己的托管资源凭证。

spec：managed resources self-fetch contract
关键决策：
- 同步 HTTP（启动期一次性调用，不需要 async）
- 失败统一抛 KeystoneSelfFetchError；调用方决定 raise vs warn
- value 强制转 str（os.environ.setdefault 只吃 str）
"""
from __future__ import annotations

import httpx

from .exceptions import KeystoneSelfFetchError

DEFAULT_TIMEOUT = 5.0

# Keystone 应用自查端点
SELF_RESOURCES_PATH = "/v1/apps/self/resources"


class SelfClient:
    """Keystone 应用自查客户端。

    用法::

        client = SelfClient(gateway_url, app_token)
        env = client.fetch_env()  # {"DB_HOST": "...", "DB_PASSWORD": "...", ...}
    """

    def __init__(
        self,
        gateway_url: str,
        app_token: str,
        timeout: float = DEFAULT_TIMEOUT,
    ) -> None:
        # rstrip("/") 兜底，避免 //v1/... 双斜杠
        self._gateway_url = gateway_url.rstrip("/")
        self._app_token = app_token
        self._timeout = timeout

    def fetch_env(self) -> dict[str, str]:
        """GET /v1/apps/self/resources，返回 env dict（key→value 都是 str）。

        失败抛 KeystoneSelfFetchError；message 中包含状态码或错误原因。
        """
        url = f"{self._gateway_url}{SELF_RESOURCES_PATH}"
        headers = {"Authorization": f"Bearer {self._app_token}"}

        try:
            with httpx.Client(timeout=self._timeout) as client:
                resp = client.get(url, headers=headers)
        except httpx.HTTPError as e:
            raise KeystoneSelfFetchError(f"network error: {e}") from e

        if resp.status_code != 200:
            body_short = (resp.text or "")[:200]
            raise KeystoneSelfFetchError(
                f"keystone returned status={resp.status_code} body={body_short}"
            )

        try:
            payload = resp.json()
        except ValueError as e:
            raise KeystoneSelfFetchError(f"invalid JSON response: {e}") from e

        code = payload.get("code")
        if code != 0:
            msg = payload.get("message", "")
            raise KeystoneSelfFetchError(
                f"keystone business error code={code} message={msg}"
            )

        data = payload.get("data") or {}
        env = data.get("env")
        if env is None:
            raise KeystoneSelfFetchError("response missing data.env field")
        if not isinstance(env, dict):
            raise KeystoneSelfFetchError(
                f"data.env must be object, got {type(env).__name__}"
            )

        # 强转 str：keystone 一般已经是 str，但 schema 上没强约束，且 os.environ 只吃 str
        return {str(k): str(v) for k, v in env.items()}
