package zdns

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/mpolden/zdns/hosts"
)

// Config specifies is the zdns configuration parameters.
type Config struct {
	DNS      DNSOptions
	Resolver ResolverOptions
	Hosts    []Hosts
}

// DNSOptions controlers the behaviour of the DNS server.
type DNSOptions struct {
	Listen              string
	Protocol            string `toml:"protocol"`
	CacheExpiryInterval string `toml:"cache_expiry_interval"`
	cacheExpiryInterval time.Duration
	CacheSize           int    `toml:"cache_size"`
	HijackMode          string `toml:"hijack_mode"`
	hijackMode          int
	RefreshInterval     string `toml:"hosts_refresh_interval"`
	refreshInterval     time.Duration
	Resolvers           []string
}

// ResolverOptions controls the behaviour of resolvers.
type ResolverOptions struct {
	Protocol string `toml:"protocol"`
	Timeout  string `toml:"timeout"`
	timeout  time.Duration
}

// Hosts controls how a hosts file should be retrieved.
type Hosts struct {
	URL     string
	Hosts   []string `toml:"entries"`
	hosts   hosts.Hosts
	Hijack  bool
	Timeout string
	timeout time.Duration
}

func (c *Config) load() error {
	var err error
	if c.DNS.Listen == "" {
		return fmt.Errorf("invalid listening address: %s", c.DNS.Listen)
	}
	if c.DNS.Protocol == "" {
		c.DNS.Protocol = "udp"
	}
	if c.DNS.Protocol != "udp" {
		return fmt.Errorf("unsupported protocol: %s", c.DNS.Protocol)
	}
	if c.DNS.CacheSize < 0 {
		return fmt.Errorf("cache size must be >= 0")
	}
	if c.DNS.CacheExpiryInterval == "" {
		c.DNS.CacheExpiryInterval = "15m"
	}
	c.DNS.cacheExpiryInterval, err = time.ParseDuration(c.DNS.CacheExpiryInterval)
	if err != nil {
		return fmt.Errorf("invalid cache expiry interval: %s", err)
	}
	switch c.DNS.HijackMode {
	case "", "zero":
		c.DNS.hijackMode = HijackZero
	case "empty":
		c.DNS.hijackMode = HijackEmpty
	case "hosts":
		c.DNS.hijackMode = HijackHosts
	default:
		return fmt.Errorf("invalid hijack mode: %s", c.DNS.HijackMode)
	}
	if c.DNS.RefreshInterval == "" {
		c.DNS.RefreshInterval = "0"
	}
	c.DNS.refreshInterval, err = time.ParseDuration(c.DNS.RefreshInterval)
	if err != nil {
		return fmt.Errorf("invalid refresh interval: %s", err)
	}
	if c.DNS.refreshInterval < 0 {
		return fmt.Errorf("refresh interval must be >= 0")
	}
	for i, hs := range c.Hosts {
		if (hs.URL == "") == (hs.Hosts == nil) {
			return fmt.Errorf("exactly one of url or hosts must be set")
		}
		if hs.URL != "" {
			url, err := url.Parse(hs.URL)
			if err != nil {
				return fmt.Errorf("%s: invalid url: %s", hs.URL, err)
			}
			switch url.Scheme {
			case "file", "http", "https":
			default:
				return fmt.Errorf("%s: unsupported scheme: %s", hs.URL, url.Scheme)
			}
			if url.Scheme == "file" && hs.Timeout != "" {
				return fmt.Errorf("%s: timeout cannot be set for %s url", hs.URL, url.Scheme)
			}
			if c.Hosts[i].Timeout == "" {
				c.Hosts[i].Timeout = "0"
			}
			c.Hosts[i].timeout, err = time.ParseDuration(c.Hosts[i].Timeout)
			if err != nil {
				return fmt.Errorf("%s: invalid timeout: %s", hs.URL, hs.Timeout)
			}
		}
		if hs.Hosts != nil {
			if hs.Timeout != "" {
				return fmt.Errorf("%s: timeout cannot be set for inline hosts", hs.Hosts)
			}
			var err error
			r := strings.NewReader(strings.Join(hs.Hosts, "\n"))
			c.Hosts[i].hosts, err = hosts.Parse(r)
			if err != nil {
				return err
			}
		}
	}
	for _, r := range c.DNS.Resolvers {
		if _, _, err := net.SplitHostPort(r); err != nil {
			return fmt.Errorf("invalid resolver: %s", err)
		}
	}
	switch c.Resolver.Protocol {
	case "udp":
		c.Resolver.Protocol = ""
	case "":
		c.Resolver.Protocol = "tcp-tls"
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
