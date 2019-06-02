package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/mpolden/zdns"
)

const (
	name       = "zdns"
	logPrefix  = name + ": "
	configName = "." + name + "rc"
)

func readConfig(name string) (zdns.Config, error) {
	if name == "" {
		home := os.Getenv("HOME")
		name = filepath.Join(home, configName)
	}
	f, err := os.Open(name)
	if err != nil {
		return zdns.Config{}, err
	}
	return zdns.ReadConfig(f)
}

func newServer(log *log.Logger, args []string) (*zdns.Server, error) {
	name := ""
	if len(args) >= 2 {
		name = args[1]
	}
	conf, err := readConfig(name)
	if err != nil {
		return nil, err
	}
	server, err := zdns.NewServer(conf)
	if err != nil {
		return nil, err
	}
	server.Logger = log
	return server, nil
}

func main() {
	log := log.New(os.Stderr, logPrefix, 0)
	srv, err := newServer(log, os.Args)
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
