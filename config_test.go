package zdns

import (
	"strings"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	text := `
listen = "0.0.0.0:53"
protocol = "udp"
cache_size = 2048
resolvers = [
  "1.1.1.1:53",
  "1.0.0.1:53",
]

[resolver]
protocol = "udp" # or: tcp, tcp-tls
timeout = "1s"

[filter]
reject_mode = "zero" # or: no-data, hosts
refresh_interval = "48h"

[[filters]]
url = "file:///home/foo/hosts-good"
reject = false

[[filters]]
url = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
reject = true
`
	r := strings.NewReader(text)
	conf, err := ReadConfig(r)
	if err != nil {
		t.Fatal(err)
	}

	var intTests = []struct {
		field string
		got   int
		want  int
	}{
		{"CacheSize", conf.CacheSize, 2048},
		{"len(Resolvers)", len(conf.Resolvers), 2},
		{"Resolver.Timeout", int(conf.Resolver.Timeout.Duration), int(time.Second)},
		{"Filter.RefreshInterval", int(conf.Filter.RefreshInterval.Duration), int(48 * time.Hour)},
		{"len(Filters)", len(conf.Filters), 2},
	}
	for _, tt := range intTests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.field, tt.got, tt.want)
		}
	}

	var stringTests = []struct {
		field string
		got   string
		want  string
	}{
		{"Listen", conf.Listen, "0.0.0.0:53"},
		{"Protocol", conf.Protocol, "udp"},
		{"Resolver.Hosts[0]", conf.Resolvers[0], "1.1.1.1:53"},
		{"Resolver.Hosts[1]", conf.Resolvers[1], "1.0.0.1:53"},
		{"Filter.RejectMode", conf.Filter.RejectMode, "zero"},
		{"Filters[0].Source", conf.Filters[0].URL, "file:///home/foo/hosts-good"},
		{"Filters[1].Source", conf.Filters[1].URL, "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"},
	}
	for _, tt := range stringTests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.field, tt.got, tt.want)
		}
	}

	var boolTests = []struct {
		field string
		got   bool
		want  bool
	}{
		{"Filters[0].Reject", conf.Filters[0].Reject, false},
		{"Filters[1].Reject", conf.Filters[1].Reject, true},
	}
	for _, tt := range boolTests {
		if tt.got != tt.want {
			t.Errorf("%s = %t, want %t", tt.field, tt.got, tt.want)
		}
	}
}
