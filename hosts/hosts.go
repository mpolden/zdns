package hosts

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
)

// LocalNames represent host names that are considered local.
var LocalNames = []string{
	"localhost",
	"localhost.localdomain",
	"local",
	"broadcasthost",
	"ip6-localhost",
	"ip6-loopback",
	"ip6-localnet",
	"ip6-mcastprefix",
	"ip6-allnodes",
	"ip6-allrouters",
	"ip6-allhosts",
	"0.0.0.0",
}

// DefaultParser is the default parser
var DefaultParser = &Parser{IgnoredHosts: LocalNames}

// Parser represents a hosts parser.
type Parser struct {
	IgnoredHosts []string
}

// Hosts represents a hosts file.
type Hosts struct {
	entries map[string][]net.IPAddr
}

// Parse uses DefaultParser to parse hosts from reader r.
func Parse(r io.Reader) (*Hosts, error) {
	return DefaultParser.Parse(r)
}

// Get returns the IP addresses of name.
func (h *Hosts) Get(name string) ([]net.IPAddr, bool) {
	ipAddrs, ok := h.entries[name]
	return ipAddrs, ok
}

// Len returns the number of entries.
func (h *Hosts) Len() int { return len(h.entries) }

func (p *Parser) ignore(name string) bool {
	for _, ignored := range p.IgnoredHosts {
		if ignored == name {
			return true
		}
	}
	return false
}

// Parse parses HOSTS from reader r.
func (p *Parser) Parse(r io.Reader) (*Hosts, error) {
	entries := make(map[string][]net.IPAddr)
	scanner := bufio.NewScanner(r)
	n := 1
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ipAddr, err := net.ResolveIPAddr("", fields[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid ip address: %s", n, fields[0])
		}
		for _, name := range fields[1:] {
			if p.ignore(name) {
				continue
			}
			entries[name] = append(entries[name], *ipAddr)
		}
		n++
	}
	return &Hosts{entries: entries}, nil
}

// Combine combines multiple Hosts into one. If name is present in multiple hosts, the first encountered name is kept
// and duplicates are discarded.
func Combine(hosts ...*Hosts) *Hosts {
	entries := make(map[string][]net.IPAddr)
	for _, h := range hosts {
		for name, ipAddr := range h.entries {
			if _, ok := entries[name]; ok {
				continue // Already added
			}
			entries[name] = ipAddr
		}
	}
	return &Hosts{entries: entries}
}

// A Matcher matches hosts entries.
type Matcher struct {
	hosts *Hosts
	next  *Matcher
}

// Match returns true if name is matches any hosts.
func (m *Matcher) Match(name string) bool {
	for m != nil {
		if m.hosts != nil {
			if _, ok := m.hosts.Get(name); ok {
				return true
			}
		}
		m = m.next
	}
	return false
}

// NewMatcher creates a matcher for given hosts.
func NewMatcher(hosts ...*Hosts) *Matcher {
	matcher := &Matcher{}
	m := matcher
	for i, h := range hosts {
		m.hosts = h
		if i < len(hosts)-1 {
			m.next = &Matcher{}
		}
		m = m.next
	}
	return matcher
}
