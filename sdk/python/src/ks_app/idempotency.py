"""幂等 LRU + uuid-v4 校验。

镜像 Go sdk/go/ksapp/idempotency.go。

设计要点：
  - 容量：默认 64（IDEMPOTENCY_LRU_CAPACITY）
  - TTL：默认 10 分钟（IDEMPOTENCY_LRU_TTL_SECONDS = 600）
  - 作用域：每 Config[T] handle 独立（Config.ensure_idemp_lru 懒初始化）
  - 命中返回 response body 字节快照；Handler 层 JSONResponse 复现
  - 进程重启清零（纯内存；on_apply 必须由业务方写幂等）

并发安全：内部 threading.Lock；put 与 get（含 TTL 过期删除）串行化。
"""
from __future__ import annotations

import re
import threading
import time
from collections import OrderedDict

# 每 handle 幂等缓存容量上限。
IDEMPOTENCY_LRU_CAPACITY: int = 64

# 每条缓存记录的存活时间（秒）。
IDEMPOTENCY_LRU_TTL_SECONDS: float = 600

# 精确 uuid-v4 正则：小写 hex + version=4 + variant ∈ {8,9,a,b}。
# 对齐 Go uuidV4Re。
_UUID_V4_RE = re.compile(
    r"^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"
)


def is_valid_idempotency_key(s: object) -> bool:
    """校验字符串是否为合法 uuid-v4。

    非 str 类型（None / int / bytes 等）一律返回 False；不抛异常。
    """
    if not isinstance(s, str):
        return False
    return bool(_UUID_V4_RE.match(s))


class IdempotencyLRU:
    """Thread-safe LRU + TTL 缓存，per Config handle 独立。

    语义：
      - put(key, value)：新增或刷新；超容量 evict 最久未用
      - get(key)：命中返回 value 并 move 到 MRU；过期或未命中返回 None（过期立删）
    """

    def __init__(
        self,
        capacity: int = IDEMPOTENCY_LRU_CAPACITY,
        ttl_seconds: float = IDEMPOTENCY_LRU_TTL_SECONDS,
    ):
        self._capacity = capacity
        self._ttl = ttl_seconds
        # items: key → (value_bytes, expires_at_wallclock)
        self._items: OrderedDict[str, tuple[bytes, float]] = OrderedDict()
        self._lock = threading.Lock()

    def put(self, key: str, value: bytes) -> None:
        """新增或刷新 key → value；超容量 evict 链表头（最久未用）。

        同 key 再 put：更新 value + 刷新 expires_at + 移到链表尾（MRU）。
        """
        with self._lock:
            expires_at = time.time() + self._ttl
            if key in self._items:
                self._items[key] = (value, expires_at)
                self._items.move_to_end(key)
                return
            self._items[key] = (value, expires_at)
            self._items.move_to_end(key)
            while len(self._items) > self._capacity:
                # evict 最久未用（OrderedDict.popitem(last=False) 弹最早插入的）
                self._items.popitem(last=False)

    def get(self, key: str) -> bytes | None:
        """查询 key；命中返回 value 并 move 到 MRU；过期立删返回 None；未命中返回 None。"""
        with self._lock:
            if key not in self._items:
                return None
            value, expires_at = self._items[key]
            if time.time() > expires_at:
                # 过期：清理后返回 miss
                del self._items[key]
                return None
            self._items.move_to_end(key)
            return value
