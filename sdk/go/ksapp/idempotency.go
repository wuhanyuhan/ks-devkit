package ksapp

// idempotency.go — 幂等 LRU + uuid-v4 校验。
//
// 规范源：docs/config-schema.md。
//
// 设计要点：
//   - 容量：默认 64（idempotencyLRUCapacity）
//   - TTL：默认 10 分钟（idempotencyLRUTTL）
//   - 作用域：每 Config[T] handle 独立（Config[T].ensureIdempLRU 懒初始化）
//   - 命中返回 response body 字节快照；Handler 层重写 header + status code
//   - 进程重启清零（纯内存；OnApply 必须由业务方写幂等）
//
// 并发安全：内部 sync.Mutex；Put 与 Get（含 TTL 过期删除）串行化。

import (
	"container/list"
	"regexp"
	"sync"
	"time"
)

// idempotencyLRUCapacity 是每 handle 幂等缓存容量上限。
const idempotencyLRUCapacity = 64

// idempotencyLRUTTL 是每条缓存记录的存活时间。
const idempotencyLRUTTL = 10 * time.Minute

// uuidV4Re 是精确 uuid-v4 正则：
//
//	^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$
//
// 只接受小写 hex + 精确 version=4 + variant ∈ {8,9,a,b}。
var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// IsValidIdempotencyKey 校验字符串是否为合法 uuid-v4 格式。
//
// 导出便于 CLI / 测试工具复用；Handler 内部亦走此校验。
func IsValidIdempotencyKey(s string) bool {
	return uuidV4Re.MatchString(s)
}

// lruEntry 是 LRU list 节点的 value；封装 key / 字节快照 / 过期时刻。
type lruEntry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

// idempotencyLRU 是 thread-safe LRU + TTL 缓存。
//
// 语义：
//   - Put(key, value)：新增或刷新，超容量 evict 最久未用
//   - Get(key)：命中返回 (value, true) 并把节点移到 MRU；过期或未命中返回 (nil, false)，
//     过期条目直接删除
//
// 作用域：每 Config[T] handle 独立（ensureIdempLRU 中懒初始化）。
type idempotencyLRU struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	ll       *list.List               // 链表头 = MRU；链表尾 = LRU
	items    map[string]*list.Element // key → ll element
}

// newIdempotencyLRU 构造一个 LRU。capacity 与 ttl 由调用方指定（生产走包常量）。
func newIdempotencyLRU(capacity int, ttl time.Duration) *idempotencyLRU {
	return &idempotencyLRU{
		capacity: capacity,
		ttl:      ttl,
		ll:       list.New(),
		items:    make(map[string]*list.Element, capacity),
	}
}

// Put 插入 key → value；已存在则更新 value、刷新位置到 MRU；超容量时 evict 最久未用。
func (l *idempotencyLRU) Put(key string, value []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()

	if el, ok := l.items[key]; ok {
		entry := el.Value.(*lruEntry)
		entry.value = value
		entry.expiresAt = now.Add(l.ttl)
		l.ll.MoveToFront(el)
		return
	}

	entry := &lruEntry{
		key:       key,
		value:     value,
		expiresAt: now.Add(l.ttl),
	}
	el := l.ll.PushFront(entry)
	l.items[key] = el

	if l.ll.Len() > l.capacity {
		// evict tail
		victim := l.ll.Back()
		if victim != nil {
			l.ll.Remove(victim)
			delete(l.items, victim.Value.(*lruEntry).key)
		}
	}
}

// Get 查询 key；命中返回 (value, true) 并刷新位置到 MRU。
// 过期条目（now > expiresAt）返回 (nil, false) 并立即删除。
func (l *idempotencyLRU) Get(key string) ([]byte, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.items[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*lruEntry)
	if time.Now().After(entry.expiresAt) {
		// 过期：清理后返回 miss
		l.ll.Remove(el)
		delete(l.items, key)
		return nil, false
	}
	l.ll.MoveToFront(el)
	return entry.value, true
}
