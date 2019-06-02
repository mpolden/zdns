package main

import (
	"io/ioutil"
	"os"
	"testing"
)

func tempFile(s string) (string, error) {
	f, err := ioutil.TempFile("", "zdns")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := ioutil.WriteFile(f.Name(), []byte(s), 0644); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func TestMain(t *testing.T) {
	conf := `
listen = "0.0.0.0:0"

[resolver]
protocol = "udp"
timeout = "1s"

[filter]
hijack_mode = "zero"
`
	f, err := tempFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)
	srv, err := newServer(nil, []string{name, f})
	if err != nil {
		t.Fatal(err)
	}
	if srv == nil {
		t.Error("want non-nil server")
	}
}
