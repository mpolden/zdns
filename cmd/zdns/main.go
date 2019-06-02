package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"
	"github.com/mpolden/zdns"
)

const (
	configName = ".zdnsrc"
)

type options struct {
	Config string `short:"f" long:"config" description:"Config file" value-name:"FILE" default:"~/.zdnsrc"`
	Log    *log.Logger
}

func (o *options) readConfig() (zdns.Config, error) {
	name := o.Config
	if o.Config == "~/"+configName {
		home := os.Getenv("HOME")
		name = filepath.Join(home, configName)
	}
	f, err := os.Open(name)
	if err != nil {
		return zdns.Config{}, err
	}
	return zdns.ReadConfig(f)
}

func main() {
	var opts options
	log := log.New(os.Stderr, "zdns: ", 0)
	p := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	if _, err := p.Parse(); err != nil {
		log.Fatal(err)
	}

	conf, err := opts.readConfig()
	if err != nil {
		log.Fatal(err)
	}
	server, err := zdns.NewServer(conf)
	if err != nil {
		log.Fatal(err)
	}
	server.Logger = log
	if err := server.ListenAndServe(conf.Listen, conf.Protocol); err != nil {
		log.Fatal(err)
	}
}
