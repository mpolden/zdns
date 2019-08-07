package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"flag"

	"github.com/mpolden/zdns"
	"github.com/mpolden/zdns/log"
)

const (
	name       = "zdns"
	logPrefix  = name + ": "
	configName = "." + name + "rc"
)

func defaultConfigFile() string { return filepath.Join(os.Getenv("HOME"), configName) }

func newServer(out io.Writer, confFile string) (*zdns.Server, error) {
	f, err := os.Open(confFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	conf, err := zdns.ReadConfig(f)
	if err != nil {
		return nil, err
	}
	log, err := log.New(out, logPrefix, conf.DNS.LogDatabase)
	if err != nil {
		return nil, err
	}
	return zdns.NewServer(log, conf)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", logPrefix, err)
	os.Exit(1)
}

func main() {
	confFile := flag.String("f", defaultConfigFile(), "config file `path`")
	help := flag.Bool("h", false, "print usage")
	flag.Parse()
	if *help {
		flag.Usage()
		return
	}
	srv, err := newServer(os.Stderr, *confFile)
	if err != nil {
		fatal(err)
	}
	if err := srv.ListenAndServe(); err != nil {
		fatal(err)
	}
}
