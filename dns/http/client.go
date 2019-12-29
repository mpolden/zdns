package http

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/miekg/dns"
)

// RFC8484 (https://tools.ietf.org/html/rfc8484) claims that application/dns-message should be used, as does
// https://developers.cloudflare.com/1.1.1.1/dns-over-https/wireformat/.
//
// However, Cloudflare's service only accept this media type from one of the older RFC drafts
// (https://tools.ietf.org/html/draft-ietf-doh-dns-over-https-05).
const mimeType = "application/dns-udpwireformat"

// Client is a DNS-over-HTTPS client.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new DNS-over-HTTPS client.
func NewClient(timeout time.Duration) *Client {
	return &Client{httpClient: &http.Client{Timeout: timeout}}
}

// Exchange sends the DNS message msg to the DNS-over-HTTPS endpoint addr and returns the response.
func (c *Client) Exchange(msg *dns.Msg, addr string) (*dns.Msg, time.Duration, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid url: %w", err)
	}

	p, err := msg.Pack()
	if err != nil {
		return nil, 0, err
	}

	r, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(p))
	if err != nil {
		return nil, 0, err
	}
	r.Header.Set("Content-Type", mimeType)
	r.Header.Set("Accept", mimeType)

	t := time.Now()
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("server returned HTTP %d error: %q", resp.StatusCode, resp.Status)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != mimeType {
		return nil, 0, fmt.Errorf("server returned unexpected ContentType %q, want %q", contentType, mimeType)
	}

	p, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	rtt := time.Since(t)
	reply := dns.Msg{}
	if err := reply.Unpack(p); err != nil {
		return nil, 0, err
	}
	return &reply, rtt, nil
}
