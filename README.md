# zdns

_zdns_ is a privacy-focused [DNS
resolver](https://en.wikipedia.org/wiki/Domain_Name_System#DNS_resolvers) and
[DNS sinkhole](https://en.wikipedia.org/wiki/DNS_sinkhole).

Its primary focus is to allow easy filtering of unwanted content at the
DNS-level, encrypt upstream requests, be portable and easy to configure.

## Features

* **Control**: Filter unwanted content at the DNS-level. Similar to
  [Pi-hole](https://github.com/pi-hole/pi-hole).
* **Fast**: Efficient filtering and caching of DNS requests.
* **Reliable**: Built with Go and [miekg/dns](https://github.com/miekg/dns) - a
  mature DNS library.
* **Secure**: Protect your DNS requests from snooping and tampering using [DNS
  over TLS](https://en.wikipedia.org/wiki/DNS_over_TLS) for upstream resolvers.
* **Self-contained**: Zero run-time dependencies make it easy to deploy and
  maintain _zdns_.
* **Observable**: _zdns_ supports DNS logging which makes it easy to observe what's
  going on your network.
* **Portable**: Run it on your VPS, laptop, Raspberry Pi or home router. Runs on
  all platforms supported by Go.

## Installation

_zdns_ is a standard Go package which can be installed as follows:

``` shell
$ go get github.com/mpolden/zdns/...
```

## Configuration

_zdns_ uses the [TOML](https://github.com/toml-lang/toml) configuration format
and expects to find its configuration file in `~/.zdnsrc` by default.

See [zdnsrc](zdnsrc) for an example configuration file.

## Usage

_zdns_ is a single self-contained binary. There is one optional command line
option, `-f`, which allows specifying a custom configuration file path.

``` shell
$ zdns -h
Usage of zdns:
  -f path
    	config file path (default "/Users/martin/.zdnsrc")
  -h	print usage
```

## Port redirection

Most operating systems expect to find their DNS resolver on UDP port 53.
However, as this is a reserved port, any program wanting to listen on this port
must have special privileges.

To work around this problem we can configure the firewall to redirect
connections to port 53 to a non-reserved port.

The following examples assumes that _zdns_ is running on port 53000 (see
`zdnsrc` for port configuration).

### Linux (iptables)

``` shell
$ iptables -t nat -A PREROUTING -d 127.0.0.1 -p udp -m udp --dport 53 -j REDIRECT --to-ports 53000
```

### macOS (pf)

1. Edit `/etc/pf.conf`
2. Add `rdr pass inet proto udp from any to 127.0.0.1 port domain -> 127.0.0.1 port 53000` below the last `rdr-anchor` line.
3. Enable PF and load rules: `pfctl -ef /etc/pf.conf`

## Why not Pi-Hole?

_This is my personal opinion and not a objective assessment of Pi-Hole._

* Pi-Hole has lots of dependencies, including a web-server and PHP, and a large
  feature scope.

* The installation script is buggy. In my personal experience, the 4.3
  installation script failed silently. This happened in both Debian stretch
  and buster LXC containers.
  
* Not packaged for any distributions.
