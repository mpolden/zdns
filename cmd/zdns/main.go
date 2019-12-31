package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"flag"

	"github.com/mpolden/zdns"
	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/dns"
	"github.com/mpolden/zdns/dns/dnsutil"
	"github.com/mpolden/zdns/http"
	"github.com/mpolden/zdns/log"
	"github.com/mpolden/zdns/signal"
)

const (
	name       = "zdns"
	logPrefix  = name + ": "
	configName = "." + name + "rc"
)

type server interface{ ListenAndServe() error }

type cli struct {
	servers []server
	wg      sync.WaitGroup
}

func defaultConfigFile() string { return filepath.Join(os.Getenv("HOME"), configName) }

func readConfig(file string) (zdns.Config, error) {
	f, err := os.Open(file)
	if err != nil {
		return zdns.Config{}, err
	}
	defer f.Close()
	return zdns.ReadConfig(f)
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", logPrefix, err)
	os.Exit(1)
}

func (c *cli) runServer(server server) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := server.ListenAndServe(); err != nil {
			fatal(err)
		}
	}()
}

func newCli(out io.Writer, args []string, configFile string, sig chan os.Signal) (*cli, error) {
	cl := flag.CommandLine
	cl.SetOutput(out)
	confFile := cl.String("f", configFile, "config file `path`")
	help := cl.Bool("h", false, "print usage")
	cl.Parse(args)
	if *help {
		cl.Usage()
		return nil, fmt.Errorf("usage option given")
	}

	// Config
	config, err := readConfig(*confFile)
	fatal(err)

	// Logger
	logger, err := log.New(out, logPrefix, log.RecordOptions{
		Mode:     config.DNS.LogMode,
		Database: config.DNS.LogDatabase,
		TTL:      config.DNS.LogTTL,
	})
	fatal(err)

	// Signal handling
	sigHandler := signal.NewHandler(sig, logger)
	sigHandler.OnClose(logger)

	// Client
	client := dnsutil.NewClient(config.Resolver.Protocol, config.Resolver.Timeout, config.DNS.Resolvers...)

	// Cache
	var cclient *dnsutil.Client
	if config.DNS.CachePrefetch {
		cclient = client
	}
	cache := cache.New(config.DNS.CacheSize, cclient)

	// DNS server
	proxy, err := dns.NewProxy(cache, client, logger)
	fatal(err)
	sigHandler.OnClose(proxy)

	dnsSrv, err := zdns.NewServer(logger, proxy, config)
	fatal(err)
	sigHandler.OnReload(dnsSrv)
	sigHandler.OnClose(dnsSrv)

	servers := []server{dnsSrv}
	// HTTP server
	if config.DNS.ListenHTTP != "" {
		httpSrv := http.NewServer(logger, cache, config.DNS.ListenHTTP)
		sigHandler.OnClose(httpSrv)
		servers = append(servers, httpSrv)
	}
	return &cli{servers: servers}, nil
}

func (c *cli) run() {
	for _, s := range c.servers {
		c.runServer(s)
	}
	c.wg.Wait()
}

func main() {
	c, err := newCli(os.Stderr, os.Args[1:], defaultConfigFile(), make(chan os.Signal, 1))
	if err == nil {
		c.run()
	}
}
