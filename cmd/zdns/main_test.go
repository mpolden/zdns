package main

import (
	"io/ioutil"
	"os"
	"syscall"
	"testing"
)

func tempFile(t *testing.T, s string) (string, error) {
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
[dns]
listen = "127.0.0.1:0"
listen_http = "127.0.0.1:0"

[resolver]
protocol = "udp"
timeout = "1s"

[filter]
hijack_mode = "zero"
`
	f, err := tempFile(t, conf)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	sig := make(chan os.Signal, 1)
	cli := newCli(ioutil.Discard, []string{"-f", f}, f, sig)
	sig <- syscall.SIGTERM
	cli.sh.Close()
}
