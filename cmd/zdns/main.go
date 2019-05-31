package main

import (
	"log"
	"os"

	"github.com/mpolden/zdns"
	"github.com/mpolden/zdns/dns"
)

func main() {
	log := log.New(os.Stderr, "zdns: ", 0)
	// TODO: Add command line options
	f, err := os.Open("/Users/martin/.zdnsrc")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	conf, err := zdns.ReadConfig(f)
	if err != nil {
		log.Fatal(err)
	}
	server, err := dns.NewServer(conf)
	if err != nil {
		log.Fatal(err)
	}
	server.Logger = log
	if err := server.ListenAndServe(":10053", "udp"); err != nil {
		log.Fatal(err)
	}
}
