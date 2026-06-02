package ksapp

import (
	"testing"
	"time"
)

// TestIdempotencyLRU_Basic Put 后 Get 应命中并返回原值。
func TestIdempotencyLRU_Basic(t *testing.T) {
	t.Parallel()
	lru := newIdempotencyLRU(4, time.Hour)
	lru.Put("k1", []byte("v1"))
	got, ok := lru.Get("k1")
	if !ok {
		t.Fatal("Get(\"k1\") 未命中")
	}
	if string(got) != "v1" {
		t.Errorf("Get(\"k1\") = %q, 期望 %q", string(got), "v1")
	}
}

// TestIdempotencyLRU_Eviction 超容量时 evict 最久未用 entry。
func TestIdempotencyLRU_Eviction(t *testing.T) {
	t.Parallel()
	lru := newIdempotencyLRU(2, time.Hour)
	lru.Put("k1", []byte("v1"))
	lru.Put("k2", []byte("v2"))
	lru.Put("k3", []byte("v3")) // 超容量 → k1 被 evict

	if _, ok := lru.Get("k1"); ok {
		t.Error("k1 应已被 evict 但仍在 LRU 中")
	}
	if _, ok := lru.Get("k2"); !ok {
		t.Error("k2 应仍存在")
	}
	if _, ok := lru.Get("k3"); !ok {
		t.Error("k3 应存在")
	}
}

// TestIdempotencyLRU_EvictionLRUOrder Get 会刷新位置，evict 真的最久未用。
func TestIdempotencyLRU_EvictionLRUOrder(t *testing.T) {
	t.Parallel()
	lru := newIdempotencyLRU(2, time.Hour)
	lru.Put("k1", []byte("v1"))
	lru.Put("k2", []byte("v2"))
	// 访问 k1 让其变成 MRU
	if _, ok := lru.Get("k1"); !ok {
		t.Fatal("k1 命中失败")
	}
	lru.Put("k3", []byte("v3")) // 应该 evict k2（最久未用）

	if _, ok := lru.Get("k1"); !ok {
		t.Error("k1 最近被访问，不应被 evict")
	}
	if _, ok := lru.Get("k2"); ok {
		t.Error("k2 应被 evict")
	}
	if _, ok := lru.Get("k3"); !ok {
		t.Error("k3 应存在")
	}
}

// TestIdempotencyLRU_TTL 超 TTL 应 Get 不命中并被删除。
func TestIdempotencyLRU_TTL(t *testing.T) {
	t.Parallel()
	lru := newIdempotencyLRU(4, 20*time.Millisecond)
	lru.Put("k1", []byte("v1"))
	time.Sleep(50 * time.Millisecond)
	if _, ok := lru.Get("k1"); ok {
		t.Error("k1 超 TTL 仍命中，期望已过期")
	}
}

// TestIdempotencyLRU_Update 重复 Put 同 key 应刷新位置且覆盖 value。
func TestIdempotencyLRU_Update(t *testing.T) {
	t.Parallel()
	lru := newIdempotencyLRU(2, time.Hour)
	lru.Put("k1", []byte("v1"))
	lru.Put("k2", []byte("v2"))
	// 更新 k1 → k1 变 MRU + value 刷新
	lru.Put("k1", []byte("v1-new"))
	lru.Put("k3", []byte("v3")) // 应 evict k2（LRU）

	got, ok := lru.Get("k1")
	if !ok {
		t.Fatal("k1 更新后 Get 失败")
	}
	if string(got) != "v1-new" {
		t.Errorf("k1 = %q, 期望 %q", string(got), "v1-new")
	}
	if _, ok := lru.Get("k2"); ok {
		t.Error("k2 应被 evict（k1 Put 刷新后 k2 变最久未用）")
	}
}

// TestIsValidIdempotencyKey_ValidUUID4 覆盖合法 uuid-v4。
func TestIsValidIdempotencyKey_ValidUUID4(t *testing.T) {
	t.Parallel()
	cases := []string{
		"123e4567-e89b-42d3-a456-426614174000", // a (variant 1xxx)
		"00000000-0000-4000-8000-000000000000", // 8 (variant 10xx)
		"ffffffff-ffff-4fff-bfff-ffffffffffff", // b
		"11111111-1111-4111-9111-111111111111", // 9
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if !IsValidIdempotencyKey(s) {
				t.Errorf("IsValidIdempotencyKey(%q) = false, 期望 true", s)
			}
		})
	}
}

// TestIsValidIdempotencyKey_InvalidUUID 覆盖各类不合法输入。
func TestIsValidIdempotencyKey_InvalidUUID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    string
	}{
		{"empty", ""},
		{"no-hyphen", "123e4567e89b42d3a456426614174000"},
		{"too-short", "123e4567-e89b-42d3-a456"},
		{"version-1", "123e4567-e89b-12d3-a456-426614174000"}, // version=1
		{"version-3", "123e4567-e89b-32d3-a456-426614174000"}, // version=3
		{"version-5", "123e4567-e89b-52d3-a456-426614174000"}, // version=5
		{"version-7", "123e4567-e89b-72d3-a456-426614174000"}, // version=7
		{"variant-c", "123e4567-e89b-42d3-c456-426614174000"}, // variant 首字符 c (非 8/9/a/b)
		{"variant-0", "123e4567-e89b-42d3-0456-426614174000"}, // variant 首字符 0
		{"upper-hex", "123E4567-E89B-42D3-A456-426614174000"}, // 大写不允许
		{"trailing-junk", "123e4567-e89b-42d3-a456-426614174000XXX"},
		{"spaces", " 123e4567-e89b-42d3-a456-426614174000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if IsValidIdempotencyKey(c.s) {
				t.Errorf("IsValidIdempotencyKey(%q) = true, 期望 false", c.s)
			}
		})
	}
}
