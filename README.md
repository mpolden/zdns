# zdns

`zdns` is a privacy-focused [DNS
sinkhole](https://en.wikipedia.org/wiki/DNS_sinkhole).

Its primary focus is to allow easy filtering of unwanted content, protect
regular DNS requests, be portable and easy to configure.

## Features

* **Control**: Filter unwanted content at the DNS-level. Similar to
  [Pi-hole](https://github.com/pi-hole/pi-hole).
* **Fast**: Efficient filtering and caching of DNS requests.
* **Reliable**: Built with Go and [miekg/dns](https://github.com/miekg/dns) - a
  mature DNS library.
* **Secure**: Protect your DNS requests from snooping and tampering using [DNS
  over TLS](https://en.wikipedia.org/wiki/DNS_over_TLS) for upstream resolvers.
* **Self-contained**: Zero run-time dependencies makes deploying and running
  `zdns` a joy.
* **Observable**: `zdns` emits metrics which makes it easy to see what's going
  on your network.
* **Portable**: Run it on your VPS, laptop, Raspberry Pi or home router. Runs on
  all platforms supported by Go.

## Configuration

`zdns` uses the [TOML](https://github.com/toml-lang/toml) configuration format
and expects to find its configuration file in `~/.zdnsrc`.

### Example

``` toml
[dns]
listen = "0.0.0.0:53"
protocol = "udp"
cache_size = 10000
# Use Cloudflare DNS servers
# https://www.cloudflare.com/learning/dns/what-is-1.1.1.1/
resolvers = [
  "1.1.1.1:853",
  "1.0.0.1:853",
]
hijack_mode = "zero"
# Refresh hosts lists every 48 hours.
hosts_refresh_interval = "48h"

[resolver]
# Use DNS over TLS
# https://developers.cloudflare.com/1.1.1.1/dns-over-tls/
protocol = "tcp-tls" # or: tcp, tcp-tls
timeout = "1s"

[[hosts]]
# Load blocklist from https://github.com/StevenBlack/hosts
url = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
hijack = true

[[hosts]]
# Whitelist some hosts that otherwise break YouTube features
entries = [
  "0.0.0.0 s.youtube.com",
  "0.0.0.0 s2.youtube.com",
]
hijack = false
```
