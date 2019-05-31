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

``` toml
servers = [
    "1.1.1.1",
    "1.0.0.1",
]
protocol = "udp" # or: tcp, https
filter_refresh_interval = "48h"

[[filters]]
source = "file:///Users/foo/hosts-trusted"
action = "accept"
format = "simple"

[[filters]]
source = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
action = "reject-null"
format = "hosts"
```


