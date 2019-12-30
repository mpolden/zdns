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
	"github.com/mpolden/zdns/log"
)

// Config specifies is the zdns configuration parameters.
type Config struct {
	DNS      DNSOptions
	Resolver ResolverOptions
	Hosts    []Hosts
}

// DNSOptions controlers the behaviour of the DNS server.
type DNSOptions struct {
	Listen          string
	Protocol        string `toml:"protocol"`
	CacheSize       int    `toml:"cache_size"`
	CachePrefetch   bool   `toml:"cache_prefetch"`
	HijackMode      string `toml:"hijack_mode"`
	hijackMode      int
	RefreshInterval string `toml:"hosts_refresh_interval"`
	refreshInterval time.Duration
	Resolvers       []string
	LogDatabase     string `toml:"log_database"`
	LogModeString   string `toml:"log_mode"`
	LogMode         int
	LogTTLString    string `toml:"log_ttl"`
	LogTTL          time.Duration
	ListenHTTP      string `toml:"listen_http"`
}

// ResolverOptions controls the behaviour of resolvers.
type ResolverOptions struct {
	Protocol      string `toml:"protocol"`
	TimeoutString string `toml:"timeout"`
	Timeout       time.Duration
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

func newConfig() Config {
	c := Config{}
	// Default values
	c.DNS.Listen = "127.0.0.1:53000"
	c.DNS.ListenHTTP = "127.0.0.1:8053"
	c.DNS.Protocol = "udp"
	c.DNS.CacheSize = 4096
	c.DNS.RefreshInterval = "48h"
	c.DNS.Resolvers = []string{
		"1.1.1.1:853",
		"1.0.0.1:853",
	}
	c.DNS.LogTTLString = "168h"
	c.Resolver.TimeoutString = "5s"
	c.Resolver.Protocol = "tcp-tls"
	return c
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
		return fmt.Errorf("invalid refresh interval: %w", err)
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
				return fmt.Errorf("%s: invalid url: %w", hs.URL, err)
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
		if c.Resolver.Protocol == "https" {
			u, err := url.Parse(r)
			if err != nil {
				return fmt.Errorf("invalid resolver %s: %w", r, err)
			}
			if u.Scheme != "https" {
				return fmt.Errorf("protocol %s requires https scheme for resolver %s", c.Resolver.Protocol, r)
			}
		} else {
			if _, _, err := net.SplitHostPort(r); err != nil {
				return fmt.Errorf("invalid resolver: %w", err)
			}
		}
	}
	if c.Resolver.Protocol == "udp" {
		c.Resolver.Protocol = "" // Empty means UDP when passed to dns.ListenAndServe
	}
	switch c.Resolver.Protocol {
	case "", "tcp", "tcp-tls", "https":
	default:
		return fmt.Errorf("invalid resolver protocol: %s", c.Resolver.Protocol)
	}
	c.Resolver.Timeout, err = time.ParseDuration(c.Resolver.TimeoutString)
	if err != nil {
		return fmt.Errorf("invalid resolver timeout: %s", c.Resolver.TimeoutString)
	}
	if c.Resolver.Timeout < 0 {
		return fmt.Errorf("resolver timeout must be >= 0")
	}
	if c.Resolver.Timeout == 0 {
		c.Resolver.Timeout = 5 * time.Second
	}
	switch c.DNS.LogModeString {
	case "":
		c.DNS.LogMode = log.ModeDiscard
	case "all":
		c.DNS.LogMode = log.ModeAll
	case "hijacked":
		c.DNS.LogMode = log.ModeHijacked
	default:
		return fmt.Errorf("invalid log mode: %s", c.DNS.LogModeString)
	}
	if c.DNS.LogModeString != "" && c.DNS.LogDatabase == "" {
		return fmt.Errorf("log_mode = %q requires log_database to be set", c.DNS.LogModeString)
	}
	if c.DNS.LogTTLString == "" {
		c.DNS.LogTTLString = "0"
	}
	c.DNS.LogTTL, err = time.ParseDuration(c.DNS.LogTTLString)
	if err != nil {
		return fmt.Errorf("invalid log TTL: %s", c.DNS.LogTTLString)
	}
	return nil
}

// ReadConfig reads a zdns configuration from reader r.
func ReadConfig(r io.Reader) (Config, error) {
	conf := newConfig()
	_, err := toml.DecodeReader(r, &conf)
	if err != nil {
		return Config{}, err
	}
	return conf, conf.load()
}
