// Package db is a second independently owned module. Its Confy parses the
// URL as part of building the component, so a bad URL is a config error at
// the module's path.
package db

import (
	"fmt"
	"net/url"

	"github.com/yiblet/confy"
)

type DB struct {
	url      *url.URL
	password string
	pool     int
}

func Open(rawURL, password string, pool int) (*DB, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "postgres" {
		return nil, fmt.Errorf("url scheme must be postgres, got %q", u.Scheme)
	}
	return &DB{url: u, password: password, pool: pool}, nil
}

func (d *DB) Host() string { return d.url.Host }

type settings struct {
	url      string
	password string
	pool     int
}

func Confy() confy.Confy[*DB] {
	return confy.Struct[settings]().
		At(func(s *settings) *string { return &s.url }, confy.String("url").Doc("postgres:// URL")).
		At(func(s *settings) *string { return &s.password }, confy.String("password").Secret()).
		At(func(s *settings) *int { return &s.pool }, confy.Int("pool").Default(10)).
		MapErr(func(s settings) (*DB, error) { return Open(s.url, s.password, s.pool) })
}
