package zdns

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	text := `
listen = "0.0.0.0:53"
listen_protocol = "udp"
cache_size = 2048
resolvers = [
  "192.0.2.1:53",
  "192.0.2.2:53",
]

[resolver]
# protocol = "tcp-tls" # or: "", "udp", "tcp"
timeout = "1s"

[filter]
hijack_mode = "zero" # or: empty, hosts
refresh_interval = "48h"

[[filters]]
url = "file:///home/foo/hosts-good"
reject = false

[[filters]]
url = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
timeout = "10s"
reject = true

[[filters]]
hosts = [
  "0.0.0.0 goodhost1",
  "0.0.0.0 goodhost2",
]
reject = false
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
		{"Resolver.Timeout", int(conf.Resolver.timeout), int(time.Second)},
		{"Filter.RefreshInterval", int(conf.Filter.refreshInterval), int(48 * time.Hour)},
		{"len(Filters)", len(conf.Filters), 3},
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
		{"Listen", conf.Listen, "0.0.0.0:53"},
		{"Protocol", conf.Protocol, "udp"},
		{"Resolver.Protocol", conf.Resolver.Protocol, "tcp-tls"},
		{"Resolver.Hosts[0]", conf.Resolvers[0], "192.0.2.1:53"},
		{"Resolver.Hosts[1]", conf.Resolvers[1], "192.0.2.2:53"},
		{"Filter.RejectMode", conf.Filter.HijackMode, "zero"},
		{"Filters[0].Source", conf.Filters[0].URL, "file:///home/foo/hosts-good"},
		{"Filters[1].Source", conf.Filters[1].URL, "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"},
		{"Filters[1].Timeout", conf.Filters[1].Timeout, "10s"},
		{"Filters[2].hosts", fmt.Sprintf("%+v", conf.Filters[2].hosts), "map[goodhost1:[{IP:0.0.0.0 Zone:}] goodhost2:[{IP:0.0.0.0 Zone:}]]"},
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
		{"Filters[0].Reject", conf.Filters[0].Reject, false},
		{"Filters[1].Reject", conf.Filters[1].Reject, true},
	}
	for i, tt := range boolTests {
		if tt.got != tt.want {
			t.Errorf("#%d: %s = %t, want %t", i, tt.field, tt.got, tt.want)
		}
	}
}

func TestConfigErrors(t *testing.T) {
	baseConf := "listen = \"0.0.0.0:53\"\n"
	conf1 := baseConf + "cache_size = -1"
	conf2 := baseConf + `
[filter]
hijack_mode = "foo"
`
	conf3 := baseConf + `
[filter]
refresh_interval = "foo"
`
	conf4 := baseConf + `
[filter]
refresh_interval = "-1h"
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
[[filters]]
url = ":foo"
`
	conf10 := baseConf + `
[[filters]]
url = "foo://bar"
`
	conf11 := baseConf + `
[[filters]]
url = "file:///tmp/foo"
timeout = "1s"
`

	conf12 := baseConf + `
[[filters]]
hosts = ["0.0.0.0 host1"]
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
