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
protocol = "udp"
cache_size = 2048
resolvers = [
  "192.0.2.1:53",
  "192.0.2.2:53=example.com",
]
hijack_mode = "zero" # or: empty, hosts
hosts_refresh_interval = "48h"
database = "/tmp/log.db"
log_mode = "all"
log_ttl = "72h"

[resolver]
protocol = "tcp-tls" # or: "", "udp", "tcp"
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
		{"Resolver.Timeout", int(conf.Resolver.Timeout), int(time.Second)},
		{"DNS.RefreshInterval", int(conf.DNS.refreshInterval), int(48 * time.Hour)},
		{"len(Hosts)", len(conf.Hosts), 3},
		{"DNS.LogTTL", int(conf.DNS.LogTTL), int(72 * time.Hour)},
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
		{"DNS.Resolvers[1]", conf.DNS.Resolvers[1], "192.0.2.2:53=example.com"},
		{"DNS.HijackMode", conf.DNS.HijackMode, "zero"},
		{"DNS.Database", conf.DNS.Database, "/tmp/log.db"},
		{"DNS.LogMode", conf.DNS.LogModeString, "all"},
		{"DNS.LogTTL", conf.DNS.LogTTLString, "72h"},
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
	conf0 := baseConf + "cache_size = -1"
	conf1 := baseConf + `
hijack_mode = "foo"
`
	conf2 := baseConf + `
hosts_refresh_interval = "foo"
`
	conf3 := baseConf + `
hosts_refresh_interval = "-1h"
`
	conf4 := baseConf + `
resolvers = ["foo"]
`
	conf5 := baseConf + `
[resolver]
protocol = "foo"
`
	conf6 := baseConf + `
[resolver]
timeout = "foo"
`
	conf7 := baseConf + `
[resolver]
timeout = "-1s"
`
	conf8 := baseConf + `
[[hosts]]
url = ":foo"
`
	conf9 := baseConf + `
[[hosts]]
url = "foo://bar"
`
	conf10 := baseConf + `
[[hosts]]
url = "file:///tmp/foo"
timeout = "1s"
`
	conf11 := baseConf + `
[[hosts]]
entries = ["0.0.0.0 host1"]
timeout = "1s"
`
	conf12 := baseConf + `
log_mode = "foo"

[resolver]
timeout = "1s"
`
	conf13 := baseConf + `
log_mode = "hijacked"

[resolver]
timeout = "1s"
`
	conf14 := baseConf + `
resolvers = ["http://example.com"]
[resolver]
protocol = "https"
`
	conf15 := baseConf + `
cache_persist = true
`
	var tests = []struct {
		in  string
		err string
	}{

		{conf0, "cache size must be >= 0"},
		{conf1, "invalid hijack mode: foo"},
		{conf2, "invalid refresh interval: time: invalid duration \"foo\""},
		{conf3, "refresh interval must be >= 0"},
		{conf4, "invalid resolver: address foo: missing port in address"},
		{conf5, "invalid resolver protocol: foo"},
		{conf6, "invalid resolver timeout: foo"},
		{conf7, "resolver timeout must be >= 0"},
		{conf8, ":foo: invalid url: parse \":foo\": missing protocol scheme"},
		{conf9, "foo://bar: unsupported scheme: foo"},
		{conf10, "file:///tmp/foo: timeout cannot be set for file url"},
		{conf11, "[0.0.0.0 host1]: timeout cannot be set for inline hosts"},
		{conf12, "invalid log mode: foo"},
		{conf13, `log_mode = "hijacked" requires 'database' to be set`},
		{conf14, "protocol https requires https scheme for resolver http://example.com"},
		{conf15, "cache_persist = true requires 'database' to be set"},
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
