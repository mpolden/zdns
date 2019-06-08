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
type Hosts map[string][]net.IPAddr

// Parse uses DefaultParser to parse hosts from reader r.
func Parse(r io.Reader) (Hosts, error) {
	return DefaultParser.Parse(r)
}

// Get returns the IP addresses of name.
func (h Hosts) Get(name string) ([]net.IPAddr, bool) {
	ipAddrs, ok := h[name]
	return ipAddrs, ok
}

// Del deletes the hosts entry of name.
func (h Hosts) Del(name string) {
	delete(h, name)
}

func (p *Parser) ignore(name string) bool {
	for _, ignored := range p.IgnoredHosts {
		if ignored == name {
			return true
		}
	}
	return false
}

// Parse parses hosts from reader r.
func (p *Parser) Parse(r io.Reader) (Hosts, error) {
	entries := make(map[string][]net.IPAddr)
	scanner := bufio.NewScanner(r)
	n := 0
	for scanner.Scan() {
		n++
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := fields[0]
		if strings.HasPrefix(ip, "#") {
			continue
		}
		ipAddr, err := net.ResolveIPAddr("", ip)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid ip address: %s - %s", n, fields[0], line)
		}
		for _, name := range fields[1:] {
			if strings.HasPrefix(name, "#") {
				break
			}
			if p.ignore(name) {
				continue
			}
			entries[name] = append(entries[name], *ipAddr)
		}
	}
	return entries, nil
}
