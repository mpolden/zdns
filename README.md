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
* **Observable**: `zdns` provides DNS logging which makes it easy to observe
  what's going on your network.
* **Portable**: Run it on your VPS, laptop, Raspberry Pi or home router. Runs on
  all platforms supported by Go.

## Configuration

`zdns` uses the [TOML](https://github.com/toml-lang/toml) configuration format
and expects to find its configuration file in `~/.zdnsrc`.

### Example

```toml
[dns]
# Listening address of this resolver.
listen = "0.0.0.0:53"

# Listening protocol. Defaults to "udp", the only supported protocol.
protocol = "udp"

# Maxium number of entries to keep in the DNS cache. The cache discards older
# entries once the number of entries exceeds this size.
cache_size = 10000

# Upstream DNS servers to use when resolving queries.
#
# This example uses Cloudflare DNS servers, which support DNS-over-TLS.
# https://www.cloudflare.com/learning/dns/what-is-1.1.1.1/
resolvers = [
  "1.1.1.1:853",
  "1.0.0.1:853",
]

# Configure how to answer hijacked DNS requests.
# Possible values:
# zero: Answer A quiries with the IPv4 zero address (0.0.0.0).
#       Answer AAAA requests with the IPv6 zero address (::).
#       This is the default.
# empty: Answer all hijacked requests with an empty answer.
# hosts: Answer hijacked requests from inline hosts (see below).
hijack_mode = "zero"

# Configures how often remote hosts lists should be refreshed. This option has 
# no default value.
hosts_refresh_interval = "48h"

# Path to the log database. Configuring a path here will enable logging of DNS
# requests. Default is empty string (no logging).
log_database = "/tmp/pfdns.db"

# Configure which requests to log.
# Possible values:
# all: Logs all requests.
# hijacked: Logs only hijacked requests (default).
# disabled: Disable logging.
log_mode = "hijacked"

[resolver]
# Set the protocol to use when sending requests to upstream resolvers. Defaults to "udp".
# Possible values:
# tcp-tls: Use encrypted protocol (DNS-over-TLS). Note that the configured upstream resolvers must support this protocol.
# udp: Plain DNS over UDP.
# tcp: Plain DNS over TCP.
protocol = "tcp-tls"

# Set the maximum timeout for a single DNS request.
timeout = "1s"

[[hosts]]
# Load hosts from an URL. No default.
url = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
# Whether to hijack DNS requests matching hostnames in this hosts list.
# true: Matching requests will be answered according to hijack_mode.
# false: Matching requests will never be hijacked.
hijack = true

[[hosts]]
# Inline hosts list. Useful for whitelisting particular hosts. No default.
entries = [
  # Whitelist some hosts that otherwise break YouTube features
  "0.0.0.0 s.youtube.com",
  "0.0.0.0 s2.youtube.com",
]
hijack = false
```
