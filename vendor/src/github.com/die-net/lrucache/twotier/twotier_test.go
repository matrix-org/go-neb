package twotier

import (
	"github.com/die-net/lrucache"
	"github.com/gregjones/httpcache"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestInterface(t *testing.T) {
	var h httpcache.Cache
	h = twoNew(1000000, 1000000)
	if assert.NotNil(t, h) {
		_, ok := h.Get("missing")
		assert.Equal(t, ok, false)
	}
}

func TestNew(t *testing.T) {
	// Check New's validation.
	c := lrucache.New(1000000, 0)
	assert.Nil(t, New(nil, nil))
	assert.Nil(t, New(nil, c))
	assert.Nil(t, New(c, nil))
	assert.Nil(t, New(c, c))

	assert.NotNil(t, New(c, lrucache.New(1000000, 0)))
}

func TestGet(t *testing.T) {
	c := twoNew(1000000, 1000000)

	// Try a cache miss.
	_, ok := c.Get("foo")
	assert.Equal(t, ok, false)

	// Add something to secondary cache, and make sure we can see it.
	c.second.Set("foo", []byte("bar"))
	v, _ := c.Get("foo")
	assert.Equal(t, string(v), "bar")

	// And it should've been written to first.
	v, _ = c.first.Get("foo")
	assert.Equal(t, string(v), "bar")

	// Change secondary cache and we should still see old value.
	c.second.Set("foo", []byte("qux"))
	v, _ = c.Get("foo")
	assert.Equal(t, string(v), "bar")

	// Pretend first expired that value and we should see new value.
	c.first.Delete("foo")
	v, _ = c.Get("foo")
	assert.Equal(t, string(v), "qux")
}

func TestSet(t *testing.T) {
	c := twoNew(1000000, 1000000)

	// Check that Set correctly overwrites second and deletes first.
	c.first.Set("foo", []byte("bar"))
	c.second.Set("foo", []byte("baz"))
	c.Set("foo", []byte("qux"))
	_, ok := c.first.Get("foo")
	assert.Equal(t, ok, false)
	v, _ := c.second.Get("foo")
	assert.Equal(t, string(v), "qux")
}

func TestDelete(t *testing.T) {
	c := twoNew(1000000, 1000000)

	// Check that Delete correctly deletes first and second.
	c.first.Set("foo", []byte("bar"))
	c.second.Set("foo", []byte("baz"))
	c.Delete("foo")
	_, ok := c.first.Get("foo")
	assert.Equal(t, ok, false)
	_, ok = c.second.Get("foo")
	assert.Equal(t, ok, false)
}

func twoNew(firstSize, secondSize int64) *TwoTier {
	return New(lrucache.New(firstSize, 0), lrucache.New(secondSize, 0))
}
