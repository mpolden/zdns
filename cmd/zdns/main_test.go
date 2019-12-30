package main

import (
	"io/ioutil"
	"os"
	"sync"
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

	main := cli{
		out:        ioutil.Discard,
		configFile: f,
		args:       []string{"-f", f},
		signal:     make(chan os.Signal, 1),
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		main.run()
	}()
	main.signal <- syscall.SIGTERM
	wg.Wait()
}
