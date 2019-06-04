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
	Listen    string
	Protocol  string
	CacheSize int `toml:"cache_size"`
	Filter    FilterOptions
	Filters   []Filter
	Resolver  ResolverOptions
	Resolvers []string
}

// ResolverOptions controlers the behaviour of resolvers.
type ResolverOptions struct {
	Protocol string `toml:"protocol"`
	Timeout  string `toml:"timeout"`
	timeout  time.Duration
}

// FilterOptions controls the behaviour of configured filters.
type FilterOptions struct {
	HijackMode      string `toml:"hijack_mode"`
	hijackMode      int
	RefreshInterval string `toml:"refresh_interval"`
	refreshInterval time.Duration
}

// A Filter specifies a source of DNS names and how they should be filtered.
type Filter struct {
	URL     string
	Reject  bool
	Timeout string
	timeout time.Duration
}

func (c *Config) load() error {
	if len(c.Listen) == 0 {
		return fmt.Errorf("invalid listening address: %s", c.Listen)
	}
	if len(c.Protocol) == 0 {
		c.Protocol = "udp"
	}
	if c.CacheSize < 0 {
		return fmt.Errorf("cache size must be >= 0")
	}
	switch c.Filter.HijackMode {
	case "", "zero":
		c.Filter.hijackMode = HijackZero
	case "empty":
		c.Filter.hijackMode = HijackEmpty
	case "hosts":
		c.Filter.hijackMode = HijackHosts
	default:
		return fmt.Errorf("invalid hijack mode: %s", c.Filter.HijackMode)
	}
	if c.Filter.RefreshInterval == "" {
		c.Filter.RefreshInterval = "0"
	}
	var err error
	c.Filter.refreshInterval, err = time.ParseDuration(c.Filter.RefreshInterval)
	if err != nil {
		return fmt.Errorf("invalid refresh interval: %s", err)
	}
	if c.Filter.refreshInterval < 0 {
		return fmt.Errorf("refresh interval must be >= 0")
	}
	for i, f := range c.Filters {
		if f.URL == "" {
			return fmt.Errorf("url must be set")
		}
		url, err := url.Parse(f.URL)
		if err != nil {
			return fmt.Errorf("%s: invalid url: %s", f.URL, err)
		}
		switch url.Scheme {
		case "file", "http", "https":
		default:
			return fmt.Errorf("%s: unsupported scheme: %s", f.URL, url.Scheme)
		}
		if url.Scheme == "file" && f.Timeout != "" {
			return fmt.Errorf("%s: timeout cannot be set for %s url", f.URL, url.Scheme)
		}
		if c.Filters[i].Timeout == "" {
			c.Filters[i].Timeout = "0"
		}
		c.Filters[i].timeout, err = time.ParseDuration(c.Filters[i].Timeout)
		if err != nil {
			return fmt.Errorf("%s: invalid timeout: %s", f.URL, f.Timeout)
		}
	}
	for _, r := range c.Resolvers {
		if _, _, err := net.SplitHostPort(r); err != nil {
			return fmt.Errorf("invalid resolver: %s", err)
		}
	}
	if c.Resolver.Protocol == "udp" {
		c.Resolver.Protocol = ""
	}
	switch c.Resolver.Protocol {
	case "", "tcp", "tcp-tls":
	default:
		return fmt.Errorf("invalid resolver protocol: %s", c.Resolver.Protocol)
	}
	c.Resolver.timeout, err = time.ParseDuration(c.Resolver.Timeout)
	if err != nil {
		return fmt.Errorf("invalid resolver timeout: %s", c.Resolver.Timeout)
	}
	if c.Resolver.timeout < 0 {
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
