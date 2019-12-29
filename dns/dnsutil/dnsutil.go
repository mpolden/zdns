package dnsutil

import (
	"errors"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var (
	// TypeToString contains a mapping of DNS request type to string.
	TypeToString = dns.TypeToString

	// RcodeToString contains a mapping of Mapping DNS response code to string.
	RcodeToString = dns.RcodeToString
)

// Resolver is the interface that wraps the Exchange method of a DNS client.
type Resolver interface {
	Exchange(*dns.Msg, string) (*dns.Msg, time.Duration, error)
}

// Exchange sends a DNS query to addr and returns the response. If more than one addr is given, all are queried and the
// first successful response is returned.
func Exchange(resolver Resolver, msg *dns.Msg, addr ...string) (*dns.Msg, error) {
	done := make(chan bool)
	c := make(chan *dns.Msg)
	var wg sync.WaitGroup
	wg.Add(len(addr))
	err := errors.New("addr is empty")
	for _, a := range addr {
		go func(addr string) {
			defer wg.Done()
			r, _, err1 := resolver.Exchange(msg, addr)
			if err1 != nil {
				err = err1
				return
			}
			c <- r
		}(a)
	}
	go func() {
		wg.Wait()
		done <- true
	}()
	for {
		select {
		case <-done:
			return nil, err
		case rr := <-c:
			return rr, nil
		}
	}
}

// Answers returns all values in the answer section of DNS message msg.
func Answers(msg *dns.Msg) []string {
	var answers []string
	for _, answer := range msg.Answer {
		switch v := answer.(type) {
		case *dns.A:
			answers = append(answers, v.A.String())
		case *dns.AAAA:
			answers = append(answers, v.AAAA.String())
		case *dns.MX:
			answers = append(answers, v.Mx)
		case *dns.PTR:
			answers = append(answers, v.Ptr)
		case *dns.NS:
			answers = append(answers, v.Ns)
		case *dns.CNAME:
			answers = append(answers, v.Target)
		}
	}
	return answers
}

// MinTTL returns the lowest TTL of of answer, authority and additional sections.
func MinTTL(msg *dns.Msg) time.Duration {
	var ttl uint32 = (1 << 31) - 1 // Maximum TTL from RFC 2181
	for _, answer := range msg.Answer {
		ttl = min(answer.Header().Ttl, ttl)
	}
	for _, ns := range msg.Ns {
		ttl = min(ns.Header().Ttl, ttl)
	}
	for _, extra := range msg.Extra {
		// OPT (EDNS) is a pseudo record which uses TTL field for extended RCODE and flags
		if extra.Header().Rrtype == dns.TypeOPT {
			continue
		}
		ttl = min(extra.Header().Ttl, ttl)
	}
	return time.Duration(ttl) * time.Second
}

func min(x, y uint32) uint32 {
	if x < y {
		return x
	}
	return y
}
