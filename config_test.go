package zdns

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	text := `
[dns]
listen = "0.0.0.0:53"
listen_protocol = "udp"
cache_size = 2048
resolvers = [
  "192.0.2.1:53",
  "192.0.2.2:53",
]
hijack_mode = "zero" # or: empty, hosts
hosts_refresh_interval = "48h"

[resolver]
# protocol = "tcp-tls" # or: "", "udp", "tcp"
timeout = "1s"

[[hosts]]
url = "file:///home/foo/hosts-good"
hijack = false

[[hosts]]
url = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
timeout = "10s"
hijack = true

[[hosts]]
entries = [
  "0.0.0.0 goodhost1",
  "0.0.0.0 goodhost2",
]
hijack = false
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
		{"DNS.CacheSize", conf.DNS.CacheSize, 2048},
		{"len(DNS.Resolvers)", len(conf.DNS.Resolvers), 2},
		{"Resolver.Timeout", int(conf.Resolver.timeout), int(time.Second)},
		{"DNS.RefreshInterval", int(conf.DNS.refreshInterval), int(48 * time.Hour)},
		{"len(Hosts)", len(conf.Hosts), 3},
	}
	for i, tt := range intTests {
		if tt.got != tt.want {
			t.Errorf("#%d: %s = %d, want %d", i, tt.field, tt.got, tt.want)
		}
	}

	var stringTests = []struct {
		field string
		got   string
		want  string
	}{
		{"DNS.Listen", conf.DNS.Listen, "0.0.0.0:53"},
		{"DNS.Protocol", conf.DNS.Protocol, "udp"},
		{"DNS.Resolvers[0]", conf.DNS.Resolvers[0], "192.0.2.1:53"},
		{"DNS.Resolvers[1]", conf.DNS.Resolvers[1], "192.0.2.2:53"},
		{"DNS.HijackMode", conf.DNS.HijackMode, "zero"},
		{"Resolver.Protocol", conf.Resolver.Protocol, "tcp-tls"},
		{"Hosts[0].Source", conf.Hosts[0].URL, "file:///home/foo/hosts-good"},
		{"Hosts[1].Source", conf.Hosts[1].URL, "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"},
		{"Hosts[1].Timeout", conf.Hosts[1].Timeout, "10s"},
		{"Hosts[2].hosts", fmt.Sprintf("%+v", conf.Hosts[2].hosts), "map[goodhost1:[{IP:0.0.0.0 Zone:}] goodhost2:[{IP:0.0.0.0 Zone:}]]"},
	}
	for i, tt := range stringTests {
		if tt.got != tt.want {
			t.Errorf("#%d: %s = %q, want %q", i, tt.field, tt.got, tt.want)
		}
	}

	var boolTests = []struct {
		field string
		got   bool
		want  bool
	}{
		{"Hosts[0].Hijack", conf.Hosts[0].Hijack, false},
		{"Hosts[1].Hijack", conf.Hosts[1].Hijack, true},
	}
	for i, tt := range boolTests {
		if tt.got != tt.want {
			t.Errorf("#%d: %s = %t, want %t", i, tt.field, tt.got, tt.want)
		}
	}
}

func TestConfigErrors(t *testing.T) {
	baseConf := "[dns]\nlisten = \"0.0.0.0:53\"\n"
	conf1 := baseConf + "cache_size = -1"
	conf2 := baseConf + `
hijack_mode = "foo"
`
	conf3 := baseConf + `
hosts_refresh_interval = "foo"
`
	conf4 := baseConf + `
hosts_refresh_interval = "-1h"
`
	conf5 := baseConf + `
resolvers = ["foo"]
`
	conf6 := baseConf + `
[resolver]
protocol = "foo"
`
	conf7 := baseConf + `
[resolver]
timeout = "foo"
`
	conf8 := baseConf + `
[resolver]
timeout = "-1s"
`
	conf9 := baseConf + `
[[hosts]]
url = ":foo"
`
	conf10 := baseConf + `
[[hosts]]
url = "foo://bar"
`
	conf11 := baseConf + `
[[hosts]]
url = "file:///tmp/foo"
timeout = "1s"
`

	conf12 := baseConf + `
[[hosts]]
entries = ["0.0.0.0 host1"]
timeout = "1s"
`
	var tests = []struct {
		in  string
		err string
	}{

		{"", "invalid listening address: "},
		{conf1, "cache size must be >= 0"},
		{conf2, "invalid hijack mode: foo"},
		{conf3, "invalid refresh interval: time: invalid duration foo"},
		{conf4, "refresh interval must be >= 0"},
		{conf5, "invalid resolver: address foo: missing port in address"},
		{conf6, "invalid resolver protocol: foo"},
		{conf7, "invalid resolver timeout: foo"},
		{conf8, "resolver timeout must be >= 0"},
		{conf9, ":foo: invalid url: parse :foo: missing protocol scheme"},
		{conf10, "foo://bar: unsupported scheme: foo"},
		{conf11, "file:///tmp/foo: timeout cannot be set for file url"},
		{conf12, "[0.0.0.0 host1]: timeout cannot be set for inline hosts"},
	}
	for i, tt := range tests {
		var got string
		_, err := ReadConfig(strings.NewReader(tt.in))
		if err != nil {
			got = err.Error()
		}
		if got != tt.err {
			t.Errorf("#%d: want %q, got %q", i, tt.err, got)
		}
	}

}
