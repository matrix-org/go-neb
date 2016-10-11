package lrucache

import (
	"github.com/gregjones/httpcache"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"runtime"
	"strconv"
	"testing"
	"time"
)

var entries = []struct {
	key   string
	value string
}{
	{"1", "one"},
	{"2", "two"},
	{"3", "three"},
	{"4", "four"},
	{"5", "five"},
}

func TestInterface(t *testing.T) {
	var h httpcache.Cache
	h = New(1000000, 0)
	if assert.NotNil(t, h) {
		_, ok := h.Get("missing")
		assert.False(t, ok)
	}
}

func TestCache(t *testing.T) {
	c := New(1000000, 0)

	for _, e := range entries {
		c.Set(e.key, []byte(e.value))
	}

	c.Delete("missing")
	_, ok := c.Get("missing")
	assert.False(t, ok)

	for _, e := range entries {
		value, ok := c.Get(e.key)
		if assert.True(t, ok) {
			assert.Equal(t, string(e.value), string(value))
		}
	}

	for _, e := range entries {
		c.Delete(e.key)

		_, ok := c.Get(e.key)
		assert.False(t, ok)
	}
}

func TestSize(t *testing.T) {
	c := New(1000000, 0)
	assert.Equal(t, int64(0), c.size)

	// Check that size is overhead + len(key) + len(value)
	c.Set("some", []byte("text"))
	assert.Equal(t, int64(entryOverhead+8), c.size)

	// Replace key
	c.Set("some", []byte("longer text"))
	assert.Equal(t, int64(entryOverhead+15), c.size)

	assert.Equal(t, c.size, c.Size())

	c.Delete("some")
	assert.Equal(t, int64(0), c.size)
}

func TestMaxSize(t *testing.T) {
	c := New(entryOverhead*2+20, 0)

	for _, e := range entries {
		c.Set(e.key, []byte(e.value))
	}

	// Make sure only the last two entries were kept.
	assert.Equal(t, int64(entryOverhead*2+10), c.size)
}

func TestMaxAge(t *testing.T) {
	c := New(1000000, 86400)

	now := time.Now().Unix()
	expected := now + 86400

	// Add one expired entry
	c.Set("foo", []byte("bar"))
	c.lru.Back().Value.(*entry).expires = now

	// Set a few and verify expiration times
	for _, s := range entries {
		c.Set(s.key, []byte(s.value))
		e := c.lru.Back().Value.(*entry)
		assert.True(t, e.expires >= expected && e.expires <= expected+10)
	}

	// Make sure we can get them all
	for _, s := range entries {
		_, ok := c.Get(s.key)
		assert.True(t, ok)
	}

	// Make sure only non-expired entries are still in the cache
	assert.Equal(t, int64(entryOverhead*5+24), c.size)

	// Expire all entries
	for _, s := range entries {
		le, ok := c.cache[s.key]
		if assert.True(t, ok) {
			le.Value.(*entry).expires = now
		}
	}

	// Get one expired entry, which should clear all expired entries
	_, ok := c.Get("3")
	assert.False(t, ok)
	assert.Equal(t, int64(0), c.size)
}

func TestRace(t *testing.T) {
	c := New(100000, 0)

	for worker := 0; worker < 8; worker++ {
		go testRaceWorker(c)
	}
}

func testRaceWorker(c *LruCache) {
	v := []byte("value")

	for n := 0; n < 1000; n++ {
		c.Set(randKey(100), v)
		_, _ = c.Get(randKey(200))
		c.Delete(randKey(100))
		_ = c.Size()
	}
}

func TestOverhead(t *testing.T) {
	if testing.Short() || !testing.Verbose() {
		t.SkipNow()
	}

	num := 1000000
	c := New(int64(num)*1000, 0)

	mem := readMem()

	for n := 0; n < num; n++ {
		c.Set(strconv.Itoa(n), []byte(randKey(1000000000)))
	}

	mem = readMem() - mem
	stored := c.Size() - int64(num)*entryOverhead
	t.Log("entryOverhead =", (int64(mem)-stored)/int64(num))
}

func BenchmarkSet(b *testing.B) {
	v := []byte("value")

	c := benchSetup(b, 10000000, 10000)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Set(randKey(10000), v)
		}
	})
}

func BenchmarkGet(b *testing.B) {
	c := benchSetup(b, 10000000, 10000)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = c.Get(randKey(20000))
		}
	})
}

func BenchmarkSize(b *testing.B) {
	c := benchSetup(b, 10000000, 10000)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = c.Size()
		}
	})
}

func BenchmarkSetGetDeleteSize(b *testing.B) {
	v := []byte("value")

	c := benchSetup(b, 10000000, 10000)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Set(randKey(10000), v)
			_, _ = c.Get(randKey(20000))
			c.Delete(randKey(10000))
			_ = c.Size()
		}
	})
}

func benchSetup(b *testing.B, size int64, entries int) *LruCache {
	c := New(size, 0)

	v := []byte("value")
	for i := 0; i < entries; i++ {
		c.Set(strconv.Itoa(i), v)
	}

	b.ResetTimer()

	return c
}

func randKey(n int32) string {
	return strconv.Itoa(int(rand.Int31n(n)))
}

func readMem() int64 {
	m := runtime.MemStats{}
	runtime.GC()
	runtime.ReadMemStats(&m)
	return int64(m.Alloc)
}
