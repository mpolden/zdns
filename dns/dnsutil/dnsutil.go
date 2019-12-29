package dnsutil

import (
	"time"

	"github.com/miekg/dns"
)

var (
	// TypeToString contains a mapping of DNS request type to string.
	TypeToString = dns.TypeToString

	// RcodeToString contains a mapping of Mapping DNS response code to string.
	RcodeToString = dns.RcodeToString
)

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
