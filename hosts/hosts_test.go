package hosts

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

type test struct {
	in  string
	out []string
	ok  bool
}

func testParser(p *Parser, hosts string, tests []test, t *testing.T) {
	h, err := p.Parse(strings.NewReader(hosts))
	if err != nil {
		t.Fatal(err)
	}
	for i, tt := range tests {
		ipAddrs, ok := h.Get(tt.in)
		var got []string
		for _, ipAddr := range ipAddrs {
			got = append(got, ipAddr.String())
		}
		if ok != tt.ok || !reflect.DeepEqual(got, tt.out) {
			t.Errorf("#%d: Get(%q) = (%v, %t), want (%v, %t)", i, tt.in, got, ok, tt.out, tt.ok)
		}
	}
}

func TestParse(t *testing.T) {
	in := `
# comment

  # comment with leading whitespace

incomplete-line

127.0.0.1       localhost
127.0.0.1       localhost.localdomain
127.0.0.1       local
255.255.255.255 broadcasthost
::1             localhost
::1             ip6-localhost #comment
::1             ip6-loopback # comment
fe80::1%lo0     localhost
ff00::0         ip6-localnet
ff00::0         ip6-mcastprefix
ff02::1         ip6-allnodes
ff02::2         ip6-allrouters
ff02::3         ip6-allhosts
0.0.0.0         0.0.0.0
192.0.2.1       test1 test2 test3
192.0.2.2       test4
192.0.2.3       test5
192.0.2.1       test6
`

	tests1 := []test{
		{"test1", []string{"192.0.2.1"}, true},
		{"test2", []string{"192.0.2.1"}, true},
		{"test3", []string{"192.0.2.1"}, true},
		{"test4", []string{"192.0.2.2"}, true},
		{"test5", []string{"192.0.2.3"}, true},
		{"#comment", nil, false},
		{"#", nil, false},
		{"nonexistent", nil, false},
		{"localhost", nil, false},
		{"localhost.localdomain", nil, false},
		{"local", nil, false},
		{"broadcasthost", nil, false},
		{"ip6-localhost", nil, false},
		{"ip6-loopback", nil, false},
		{"ip6-localnet", nil, false},
		{"ip6-mcastprefix", nil, false},
		{"ip6-allnodes", nil, false},
		{"ip6-allrouters", nil, false},
		{"ip6-allhosts", nil, false},
		{"0.0.0.0", nil, false},
	}

	testParser(DefaultParser, in, tests1, t)

	tests2 := []test{
		{"localhost", []string{"127.0.0.1", "::1", "fe80::1%lo0"}, true},
		{"localhost.localdomain", []string{"127.0.0.1"}, true},
		{"local", []string{"127.0.0.1"}, true},
		{"broadcasthost", []string{"255.255.255.255"}, true},
		{"ip6-localhost", []string{"::1"}, true},
		{"ip6-loopback", []string{"::1"}, true},
		{"ip6-localnet", []string{"ff00::"}, true},
		{"ip6-mcastprefix", []string{"ff00::"}, true},
		{"ip6-allnodes", []string{"ff02::1"}, true},
		{"ip6-allrouters", []string{"ff02::2"}, true},
		{"ip6-allhosts", []string{"ff02::3"}, true},
		{"0.0.0.0", []string{"0.0.0.0"}, true},
	}
	testParser(&Parser{}, in, tests2, t)
}

func TestMatch(t *testing.T) {
	m1 := Matcher{
		hosts: map[string][]net.IPAddr{
			"test1": {{IP: net.ParseIP("192.0.2.1")}},
			"test2": {{IP: net.ParseIP("192.0.2.2")}},
		},
	}
	m2 := Matcher{}
	m3 := Matcher{next: &m1}
	var tests = []struct {
		m   Matcher
		in  string
		out []net.IPAddr
		ok  bool
	}{
		{m1, "foo", nil, false}, // Non-matching name
		{m1, "test1", []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}, true}, // Matching name
		{m2, "foo", nil, false}, // Empty matcher matches nothing
		{m3, "test2", []net.IPAddr{{IP: net.ParseIP("192.0.2.2")}}, true}, // One of multiple matcher matches
	}
	for i, tt := range tests {
		ipAddrs, ok := tt.m.Match(tt.in)
		if ok != tt.ok || !reflect.DeepEqual(ipAddrs, tt.out) {
			t.Errorf("#%d: Match(%q) = (%v, %t), want (%v, %t)", i, tt.in, ipAddrs, ok, tt.out, tt.ok)
		}
	}
}

func TestNewMatcher(t *testing.T) {
	var hosts1 Hosts = map[string][]net.IPAddr{
		"test1": {{IP: net.ParseIP("192.0.2.1")}},
	}
	var hosts2 Hosts = map[string][]net.IPAddr{
		"test2": {{IP: net.ParseIP("192.0.2.2")}},
	}
	m := NewMatcher(hosts1, hosts2)
	if !reflect.DeepEqual(m.hosts, hosts1) {
		t.Errorf("got %+v, want %+v", m.hosts, hosts1)
	}
	if m.next == nil {
		t.Error("want non-nil")
	}
	if !reflect.DeepEqual(m.next.hosts, hosts2) {
		t.Errorf("got %+v, want %+v", m.next.hosts, hosts2)
	}
	if m.next.next != nil {
		t.Error("want nil leaf")
	}
}
