package zdns

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"time"

	"github.com/BurntSushi/toml"
)

// Config specifies is the zdns configuration parameters.
type Config struct {
	Listen          string
	Protocol        string
	CacheSize       int `toml:"cache_size"`
	Filter          FilterOptions
	Filters         []Filter
	ResolverTimeout duration `toml:"resolver_timeout"`
	Resolvers       []Resolver
}

// A Resolver is a recursive DNS resolver.
type Resolver struct {
	Name     string
	Protocol string
}

// FilterOptions controls the behaviour of configured filters.
type FilterOptions struct {
	RejectMode      string   `toml:"reject_mode"`
	RefreshInterval duration `toml:"refresh_interval"`
}

// A Filter specifies a source of DNS names and how they should be filtered.
type Filter struct {
	URL    hostsURL
	Reject bool
}

type hostsURL struct{ *url.URL }

type duration struct{ time.Duration }

func (u *hostsURL) UnmarshalText(text []byte) error {
	var err error
	u.URL, err = url.Parse(string(text))
	return err
}

func (d *duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func (c *Config) load() error {
	if len(c.Listen) == 0 {
		return fmt.Errorf("invalid listening address: %q", c.Listen)
	}
	if len(c.Protocol) == 0 {
		c.Protocol = "udp"
	}
	if c.CacheSize < 0 {
		return fmt.Errorf("cache size must be >= 0")
	}
	switch c.Filter.RejectMode {
	case "zero", "no-data":
	default:
		return fmt.Errorf("invalid reject mode: %q", c.Filter.RejectMode)
	}
	if c.Filter.RefreshInterval.Duration < 0 {
		return fmt.Errorf("refresh interval must be >= 0")
	}
	for _, f := range c.Filters {
		if f.URL.URL == nil {
			return fmt.Errorf("url must be set")
		}
		switch f.URL.URL.Scheme {
		case "file", "http", "https":
		default:
			return fmt.Errorf("%s: invalid scheme: %s", f.URL.URL.String(), f.URL.URL.Scheme)
		}
	}
	for i := range c.Resolvers {
		r := c.Resolvers[i]
		if _, _, err := net.SplitHostPort(r.Name); err != nil {
			return fmt.Errorf("%s: %s", r.Name, err)
		}
		if r.Protocol == "udp" {
			r.Protocol = ""
		}
		switch r.Protocol {
		case "", "tcp", "tcp-tls":
		default:
			return fmt.Errorf("%s: invalid protocol: %s", r.Name, r.Protocol)
		}
	}
	if c.ResolverTimeout.Duration < 0 {
		return fmt.Errorf("resolver timeout must be >= 0")
	}
	return nil
}

// ReadConfig reads a zdns configuration from reader r.
func ReadConfig(r io.Reader) (Config, error) {
	var conf Config
	_, err := toml.DecodeReader(r, &conf)
	if err != nil {
		return Config{}, err
	}
	return conf, conf.load()
}
