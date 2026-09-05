// Package cache is a module that owns its own configuration. It exports the
// component and a Confy that builds it; the settings struct stays private.
// Nothing here knows where in the final config tree it will be mounted, or
// what other modules exist.
package cache

import (
	"fmt"
	"time"

	"github.com/yiblet/confy"
)

// Cache is the component the rest of the program uses.
type Cache struct {
	addr string
	ttl  time.Duration
	size int
}

// New constructs a Cache; the Confy calls it once the settings are valid.
func New(addr string, ttl time.Duration, size int) (*Cache, error) {
	if ttl < time.Second {
		return nil, fmt.Errorf("ttl %s is below the 1s minimum", ttl)
	}
	return &Cache{addr: addr, ttl: ttl, size: size}, nil
}

func (c *Cache) Addr() string { return c.addr }

type settings struct {
	addr string
	ttl  time.Duration
	size int
}

// Confy is the module's whole configuration contract: what it reads, and
// how that becomes a *Cache.
func Confy() confy.Confy[*Cache] {
	return confy.Struct[settings]().
		At(func(s *settings) *string { return &s.addr }, confy.String("addr").Doc("Redis address")).
		At(func(s *settings) *time.Duration { return &s.ttl }, confy.Duration("ttl").Default(5*time.Minute)).
		At(func(s *settings) *int { return &s.size }, confy.Int("size").Default(1000).Check(positive)).
		MapErr(func(s settings) (*Cache, error) { return New(s.addr, s.ttl, s.size) })
}

func positive(n int) error {
	if n < 1 {
		return fmt.Errorf("size must be positive")
	}
	return nil
}
