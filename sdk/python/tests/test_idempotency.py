"""幂等 LRU + uuid-v4 校验单元测试。

镜像 Go sdk/go/ksapp/idempotency_test.go。覆盖：
  - is_valid_idempotency_key：合法 uuid-v4 / 大写 / 非 v4 / 长度错 / 非 str
  - IdempotencyLRU：put/get / TTL 过期 / 容量驱逐 / get move-to-front
  - 并发安全（简化：不触发 race，靠 threading.Lock）
"""
from __future__ import annotations

import time

import pytest

from ks_app.idempotency import (
    IDEMPOTENCY_LRU_CAPACITY,
    IDEMPOTENCY_LRU_TTL_SECONDS,
    IdempotencyLRU,
    is_valid_idempotency_key,
)


# ---- is_valid_idempotency_key ----


def test_is_valid_idempotency_key_lowercase_v4():
    assert is_valid_idempotency_key("123e4567-e89b-42d3-a456-426614174000") is True
    # variant: 8 / 9 / a / b 均合法
    assert is_valid_idempotency_key("123e4567-e89b-42d3-8456-426614174000") is True
    assert is_valid_idempotency_key("123e4567-e89b-42d3-9456-426614174000") is True
    assert is_valid_idempotency_key("123e4567-e89b-42d3-a456-426614174000") is True
    assert is_valid_idempotency_key("123e4567-e89b-42d3-b456-426614174000") is True


def test_is_valid_idempotency_key_uppercase_rejected():
    """精确小写 hex uuid-v4。大写应被拒。"""
    assert is_valid_idempotency_key("123E4567-E89B-42D3-A456-426614174000") is False


def test_is_valid_idempotency_key_wrong_version():
    # v1 时间戳格式
    assert is_valid_idempotency_key("123e4567-e89b-12d3-a456-426614174000") is False
    # v3 MD5
    assert is_valid_idempotency_key("123e4567-e89b-32d3-a456-426614174000") is False


def test_is_valid_idempotency_key_wrong_variant():
    # variant 必须在 {8,9,a,b}；7 不合法
    assert is_valid_idempotency_key("123e4567-e89b-42d3-7456-426614174000") is False
    # c / d / e / f 不合法
    assert is_valid_idempotency_key("123e4567-e89b-42d3-c456-426614174000") is False


def test_is_valid_idempotency_key_wrong_length():
    assert is_valid_idempotency_key("") is False
    assert is_valid_idempotency_key("123e4567-e89b-42d3-a456-42661417400") is False
    assert is_valid_idempotency_key("123e4567-e89b-42d3-a456-4266141740000") is False


def test_is_valid_idempotency_key_non_string():
    assert is_valid_idempotency_key(None) is False  # type: ignore[arg-type]
    assert is_valid_idempotency_key(123) is False  # type: ignore[arg-type]
    assert is_valid_idempotency_key(b"123e4567-e89b-42d3-a456-426614174000") is False  # type: ignore[arg-type]


# ---- IdempotencyLRU ----


def test_lru_put_then_get_returns_same_value():
    lru = IdempotencyLRU(capacity=4, ttl_seconds=60)
    lru.put("key-1", b"value-1")
    assert lru.get("key-1") == b"value-1"


def test_lru_get_miss_returns_none():
    lru = IdempotencyLRU(capacity=4, ttl_seconds=60)
    assert lru.get("missing") is None


def test_lru_put_updates_existing_value_without_evicting_others():
    lru = IdempotencyLRU(capacity=2, ttl_seconds=60)
    lru.put("a", b"va")
    lru.put("b", b"vb")
    lru.put("a", b"va2")  # 更新 a → 只刷新 a；b 不应被 evict
    assert lru.get("a") == b"va2"
    assert lru.get("b") == b"vb"


def test_lru_eviction_when_over_capacity():
    """容量 2，put 3 个 key，最老的应被 evict。"""
    lru = IdempotencyLRU(capacity=2, ttl_seconds=60)
    lru.put("a", b"va")
    lru.put("b", b"vb")
    lru.put("c", b"vc")  # 插入 c 触发 evict
    assert lru.get("a") is None  # a 被 evict（最老）
    assert lru.get("b") == b"vb"
    assert lru.get("c") == b"vc"


def test_lru_get_moves_to_mru():
    """get(A) 后 put(C) 容量 2 → 此时 B 是最久未用应被 evict，A 因 get 被 move-to-front 保留。"""
    lru = IdempotencyLRU(capacity=2, ttl_seconds=60)
    lru.put("a", b"va")
    lru.put("b", b"vb")
    _ = lru.get("a")  # A 被 move-to-front
    lru.put("c", b"vc")  # evict 最久未用 → B
    assert lru.get("a") == b"va"  # A 被保活
    assert lru.get("b") is None  # B 被 evict
    assert lru.get("c") == b"vc"


def test_lru_ttl_expires_and_removes_entry():
    """ttl 很短 → sleep 超过 TTL → get 返 None 且条目被立即删除。"""
    lru = IdempotencyLRU(capacity=4, ttl_seconds=0.05)
    lru.put("key-1", b"value-1")
    assert lru.get("key-1") == b"value-1"  # 未过期
    time.sleep(0.15)
    assert lru.get("key-1") is None  # 过期 → miss
    # 再 get 一次应仍然 miss（删除已生效）
    assert lru.get("key-1") is None


def test_lru_put_refresh_resets_expiration():
    """put 同 key 更新 value 应刷新 expires_at（防止误 evict）。"""
    lru = IdempotencyLRU(capacity=4, ttl_seconds=0.1)
    lru.put("k", b"v1")
    time.sleep(0.06)  # 还未过期
    lru.put("k", b"v2")  # 刷新
    time.sleep(0.06)  # 若未刷新此时已过期
    assert lru.get("k") == b"v2"


def test_lru_default_constants_match_spec():
    """默认常量：容量 64，TTL 600 秒。"""
    assert IDEMPOTENCY_LRU_CAPACITY == 64
    assert IDEMPOTENCY_LRU_TTL_SECONDS == 600

    lru = IdempotencyLRU()
    assert lru._capacity == 64
    assert lru._ttl == 600
