package dnsutil

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestMinTTL(t *testing.T) {
	var tests = []struct {
		answer []dns.RR
		ns     []dns.RR
		extra  []dns.RR
		ttl    time.Duration
	}{
		{
			[]dns.RR{
				&dns.A{Hdr: dns.RR_Header{Ttl: 3600}},
				&dns.A{Hdr: dns.RR_Header{Ttl: 60}},
			},
			nil,
			nil,
			time.Minute,
		},
		{
			[]dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 60}}},
			[]dns.RR{&dns.NS{Hdr: dns.RR_Header{Ttl: 30}}},
			nil,
			30 * time.Second,
		},
		{
			[]dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 60}}},
			[]dns.RR{&dns.NS{Hdr: dns.RR_Header{Ttl: 30}}},
			[]dns.RR{&dns.NS{Hdr: dns.RR_Header{Ttl: 10}}},
			10 * time.Second,
		},
		{
			[]dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 60}}},
			nil,
			[]dns.RR{
				&dns.OPT{Hdr: dns.RR_Header{Ttl: 10, Rrtype: dns.TypeOPT}}, // Ignored
				&dns.A{Hdr: dns.RR_Header{Ttl: 30}},
			},
			30 * time.Second,
		},
	}
	for i, tt := range tests {
		msg := dns.Msg{}
		msg.Answer = tt.answer
		msg.Ns = tt.ns
		msg.Extra = tt.extra
		if got := MinTTL(&msg); got != tt.ttl {
			t.Errorf("#%d: MinTTL(\n%s) = %s, want %s", i, msg.String(), got, tt.ttl)
		}
	}
}
