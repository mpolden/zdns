package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"flag"

	"github.com/mpolden/zdns"
	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/dns"
	"github.com/mpolden/zdns/dns/dnsutil"
	"github.com/mpolden/zdns/http"
	"github.com/mpolden/zdns/signal"
	"github.com/mpolden/zdns/sql"
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

	// Logging and signal handling
	logger := log.New(out, logPrefix, log.Lshortfile)
	sigHandler := signal.NewHandler(sig, logger)

	// SQL backends
	var (
		sqlLogger *sql.Logger
		sqlCache  *sql.Cache
	)
	if config.DNS.Database != "" {
		sqlClient, err := sql.New(config.DNS.Database)
		fatal(err)

		// Logger
		sqlLogger = sql.NewLogger(sqlClient, config.DNS.LogMode, config.DNS.LogTTL, logger)
		sigHandler.OnClose(sqlLogger)

		// Cache
		sqlCache = sql.NewCache(sqlClient, logger)
		sigHandler.OnClose(sqlCache)
	}

	// DNS client
	dnsClient := dnsutil.NewClient(config.Resolver.Protocol, config.Resolver.Timeout, config.DNS.Resolvers...)

	// Cache
	var dnsCache *cache.Cache
	var cacheDNS *dnsutil.Client
	if config.DNS.CachePrefetch {
		cacheDNS = dnsClient
	}
	if sqlCache != nil && config.DNS.CachePersist {
		dnsCache = cache.NewWithBackend(config.DNS.CacheSize, cacheDNS, sqlCache)

	} else {
		dnsCache = cache.New(config.DNS.CacheSize, cacheDNS)
	}

	// DNS server
	proxy, err := dns.NewProxy(dnsCache, dnsClient, logger, sqlLogger)
	fatal(err)
	sigHandler.OnClose(proxy)

	dnsSrv, err := zdns.NewServer(proxy, config, logger)
	fatal(err)
	sigHandler.OnReload(dnsSrv)
	sigHandler.OnClose(dnsSrv)
	servers := []server{dnsSrv}

	// HTTP server
	if config.DNS.ListenHTTP != "" {
		httpSrv := http.NewServer(dnsCache, sqlLogger, logger, config.DNS.ListenHTTP)
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
