# zdns

![Build Status](https://github.com/mpolden/zdns/workflows/ci/badge.svg)

`zdns` is a privacy-focused [DNS
resolver](https://en.wikipedia.org/wiki/Domain_Name_System#DNS_resolvers) and
[DNS sinkhole](https://en.wikipedia.org/wiki/DNS_sinkhole).

Its primary focus is to allow easy filtering of unwanted content at the
DNS-level, transport upstream requests securely, be portable and easy to
configure.

## Contents

* [Features](#features)
* [Usage](#usage)
  * [Installation](#installation)
  * [Configuration](#configuration)
  * [Logging](#logging)
  * [Port redirection](#port-redirection)
* [REST API](#rest-api)
* [Why not Pi-hole?](#why-not-pi-hole)

## Features

* **Control**: Filter unwanted content at the DNS-level. Similar to
  [Pi-hole](https://github.com/pi-hole/pi-hole).
* **Fast**: Parallel resolving over multiple resolvers, efficient filtering and
  caching of DNS requests. With pre-fetching enabled, cached requests will never
  block waiting for the upstream resolver. Asynchronous persistent caching is
  also supported.
* **Reliable**: Built with Go and [miekg/dns](https://github.com/miekg/dns) - a
  mature DNS library.
* **Secure**: Protect your DNS requests from snooping and tampering using [DNS
  over TLS](https://en.wikipedia.org/wiki/DNS_over_TLS) or [DNS over
  HTTPS](https://en.wikipedia.org/wiki/DNS_over_HTTPS) for upstream resolvers.
* **Self-contained**: Zero run-time dependencies makes `zdns` easy to deploy and
  maintain.
* **Observable**: `zdns` features DNS logging and metrics which makes it easy to
  observe what's going on your network.
* **Portable**: Run it on your VPS, container, laptop, Raspberry Pi or home
  router. Runs on all platforms supported by Go.

## Usage

### Installation

`zdns` is a standard Go package. Use `go get` to install it.

``` shell
$ go get github.com/mpolden/zdns/...
```

### Configuration

`zdns` uses the [TOML](https://github.com/toml-lang/toml) configuration format
and expects to find its configuration file in `~/.zdnsrc` by default.

See [zdnsrc](zdnsrc) for an example configuration file.
[zdns.service](zdns.service) contains an example systemd service file.

An optional command line option, `-f`, allows specifying a custom configuration
file path.

### Logging

`zdns` supports logging of DNS requests. Logs are written to a SQLite database.

Logs can be inspected through the built-in REST API or by querying the SQLite
database directly. See `zdnsrc` for more details.

### Port redirection

Most operating systems expect to find their DNS resolver on UDP port 53.
However, as this is a well-known port, any program listening on this port must
have special privileges.

To work around this problem we can configure the firewall to redirect
connections to port 53 to a non-reserved port.

The following examples assumes that `zdns` is running on port 53000. See
`zdnsrc` for port configuration.

#### Linux (iptables)

``` shell
# External requests
$ iptables -t nat -A PREROUTING -d -p udp -m udp --dport 53 -j REDIRECT --to-ports 53000

# Local requests
$ iptables -A OUTPUT -d 127.0.0.1 -p udp -m udp --dport 53 -j REDIRECT --to-ports 53000
```

#### macOS (pf)

1. Edit `/etc/pf.conf`
2. Add `rdr pass inet proto udp from any to 127.0.0.1 port domain -> 127.0.0.1 port 53000` below the last `rdr-anchor` line.
3. Enable PF and load rules: `pfctl -ef /etc/pf.conf`

## REST API

A basic REST API provides access to request log and cache entries. The API is
served by the built-in web server, which can be enabled in `zdnsrc`.

### Examples

Read the log:
```shell
$ curl -s 'http://127.0.0.1:8053/log/v1/?n=1' | jq .
[
  {
    "time": "2019-12-27T10:43:23Z",
    "remote_addr": "127.0.0.1",
    "hijacked": false,
    "type": "AAAA",
    "question": "discovery.syncthing.net.",
    "answers": [
      "2400:6180:100:d0::741:a001",
      "2a03:b0c0:0:1010::bb:4001"
    ]
  }
]
```

Read the cache:
```shell
$ curl -s 'http://127.0.0.1:8053/cache/v1/?n=1' | jq .
[
  {
    "time": "2019-12-27T10:46:11Z",
    "ttl": 18,
    "type": "A",
    "question": "gateway.fe.apple-dns.net.",
    "answers": [
      "17.248.150.110",
      "17.248.150.113",
      "17.248.150.10",
      "17.248.150.40",
      "17.248.150.42",
      "17.248.150.51",
      "17.248.150.79",
      "17.248.150.108"
    ],
    "rcode": "NOERROR"
  }
]
```

Clear the cache:
```shell
$ curl -s -XDELETE 'http://127.0.0.1:8053/cache/v1/' | jq .
{
  "message": "Cleared cache."
}
```

Metrics:

``` shell
$ curl 'http://127.0.0.1:8053/metric/v1/?resolution=1m' | jq .
{
  "summary": {
    "log": {
      "since": "2020-01-05T00:58:49Z",
      "total": 3816,
      "hijacked": 874,
      "pending_tasks": 0
    },
    "cache": {
      "size": 845,
      "capacity": 4096,
      "pending_tasks": 0,
      "backend": {
        "pending_tasks": 0
      }
    }
  },
  "requests": [
    {
      "time": "2020-01-05T00:58:49Z",
      "count": 1
    }
  ]
}
```

Note that `log_mode = "hijacked"` or `log_mode = "all"` is required to make
metrics available. Choosing `hijacked` will only produce metrics for hijacked
requests.

The query parameter `resolution` controls the resolution of the data points in
`requests`. It accepts the same values as
[time.ParseDuration](https://golang.org/pkg/time/#ParseDuration) and defaults to
`1m`.

## Why not Pi-hole?

_This is my personal opinion and not a objective assessment of Pi-hole._

* Pi-hole has lots of dependencies and a large feature scope.

* Buggy installation script. In my personal experience, the 4.3 installation
  script failed silently in both Debian stretch and buster LXC containers.

* Installation method pipes `curl` to `bash`. Not properly packaged for any
  distributions.
