package main

import (
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

func init() {
	log.SetPrefix(logPrefix)
	log.SetFlags(log.Lshortfile)
}

type server interface{ ListenAndServe() error }

type cli struct {
	servers []server
	sh      *signal.Handler
	wg      sync.WaitGroup
}

func configPath() string { return filepath.Join(os.Getenv("HOME"), configName) }

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
	log.Fatal(err)
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

func newCli(out io.Writer, args []string, configFile string, sig chan os.Signal) *cli {
	cl := flag.CommandLine
	cl.SetOutput(out)
	log.SetOutput(out)
	confFile := cl.String("f", configFile, "config file `path`")
	cl.Parse(args)

	// Config
	config, err := readConfig(*confFile)
	fatal(err)

	// Signal handler
	sigHandler := signal.NewHandler(sig)

	// SQL backends
	var (
		sqlClient *sql.Client
		sqlLogger *sql.Logger
		sqlCache  *sql.Cache
	)
	if config.DNS.Database != "" {
		sqlClient, err = sql.New(config.DNS.Database)
		fatal(err)

		// Logger
		sqlLogger = sql.NewLogger(sqlClient, config.DNS.LogMode, config.DNS.LogTTL)

		// Cache
		sqlCache = sql.NewCache(sqlClient)
	}

	// DNS client
	dnsConfig := dnsutil.Config{
		Network: config.Resolver.Protocol,
		Timeout: config.Resolver.Timeout,
	}
	dnsClients := make([]dnsutil.Client, 0, len(config.DNS.Resolvers))
	for _, addr := range config.DNS.Resolvers {
		dnsClients = append(dnsClients, dnsutil.NewClient(addr, dnsConfig))
	}
	dnsClient := dnsutil.NewMux(dnsClients...)

	// Cache
	var dnsCache *cache.Cache
	var cacheDNS dnsutil.Client
	if config.DNS.CachePrefetch {
		cacheDNS = dnsClient
	}
	if sqlCache != nil && config.DNS.CachePersist {
		dnsCache = cache.NewWithBackend(config.DNS.CacheSize, cacheDNS, sqlCache)

	} else {
		dnsCache = cache.New(config.DNS.CacheSize, cacheDNS)
	}

	// DNS server
	proxy, err := dns.NewProxy(dnsCache, dnsClient, sqlLogger)
	fatal(err)

	dnsSrv, err := zdns.NewServer(proxy, config)
	fatal(err)
	sigHandler.OnReload(dnsSrv)
	servers := []server{dnsSrv}

	// HTTP server
	var httpSrv *http.Server
	if config.DNS.ListenHTTP != "" {
		httpSrv = http.NewServer(dnsCache, sqlLogger, sqlCache, config.DNS.ListenHTTP)
		servers = append(servers, httpSrv)
	}

	// Close proxy first
	sigHandler.OnClose(proxy)

	// ... then HTTP server
	if httpSrv != nil {
		sigHandler.OnClose(httpSrv)
	}

	// ... then cache
	sigHandler.OnClose(dnsCache)

	// ... then database components
	if config.DNS.Database != "" {
		sigHandler.OnClose(sqlLogger)
		sigHandler.OnClose(sqlCache)
		sigHandler.OnClose(sqlClient)
	}

	// ... and finally the server itself
	sigHandler.OnClose(dnsSrv)
	return &cli{servers: servers, sh: sigHandler}
}

func (c *cli) run() {
	for _, s := range c.servers {
		c.runServer(s)
	}
	c.wg.Wait()
	c.sh.Close()
}

func main() {
	sig := make(chan os.Signal, 1)
	c := newCli(os.Stderr, os.Args[1:], configPath(), sig)
	c.run()
}
