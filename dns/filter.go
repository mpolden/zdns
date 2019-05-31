package dns

import (
	"bufio"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type readURL func(*url.URL) (io.ReadCloser, error)

type filterList struct {
	readURL
	reject bool
	URL    *url.URL
}

func (l *filterList) Load() (map[string]bool, error) {
	rc, err := l.readURL(l.URL)
	if err != nil {
		return nil, err
	}
	return parseList(rc, l.reject)
}

func newFilterList(url *url.URL, reject bool) (*filterList, error) {
	var readURL readURL
	switch url.Scheme {
	case "file":
		readURL = fileOpen
	case "http", "https":
		readURL = httpOpen
	}
	return &filterList{
		URL:     url,
		readURL: readURL,
		reject:  reject,
	}, nil
}

func fileOpen(url *url.URL) (io.ReadCloser, error) { return os.Open(url.Path) }

func httpOpen(url *url.URL) (io.ReadCloser, error) {
	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(url.String())
	if err != nil {
		return nil, err
	}
	return res.Body, err
}

func parseList(rc io.ReadCloser, reject bool) (map[string]bool, error) {
	defer rc.Close()
	result := make(map[string]bool)
	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[1]
		switch name {
		case "localhost",
			"localhost.localdomain",
			"local",
			"broadcasthost",
			"ip6-localhost",
			"ip6-loopback",
			"ip6-localnet",
			"ip6-mcastprefix",
			"ip6-allnodes",
			"ip6-allrouters",
			"ip6-allhosts",
			"0.0.0.0":
			continue
		}
		result[name] = reject
	}
	return result, nil
}
