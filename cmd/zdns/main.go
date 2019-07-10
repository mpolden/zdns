package main

import (
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

func newServer(log *log.Logger, confFile string) (*zdns.Server, error) {
	f, err := os.Open(confFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	conf, err := zdns.ReadConfig(f)
	if err != nil {
		return nil, err
	}
	server, err := zdns.NewServer(log, conf)
	if err != nil {
		return nil, err
	}
	return server, nil
}

func main() {
	conf := flag.String("f", defaultConfigFile(), "config file `path`")
	help := flag.Bool("h", false, "print usage")
	flag.Parse()
	if *help {
		flag.Usage()
		return
	}

	log := log.New(os.Stderr, logPrefix)
	srv, err := newServer(log, *conf)
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
