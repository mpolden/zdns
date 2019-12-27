package http

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Reponse and request data from https://developers.cloudflare.com/1.1.1.1/dns-over-https/wireformat/.
const response = `
00 00 81 80 00 01 00 01  00 00 00 00 03 77 77 77
07 65 78 61 6d 70 6c 65  03 63 6f 6d 00 00 01 00
01 03 77 77 77 07 65 78  61 6d 70 6c 65 03 63 6f
6d 00 00 01 00 01 00 00  00 80 00 04 C0 00 02 01
`

const request = `
00 00 01 00 00 01 00 00  00 00 00 00 03 77 77 77
07 65 78 61 6d 70 6c 65  03 63 6f 6d 00 00 01 00
01
`

func hexDecode(s string) []byte {
	replacer := strings.NewReplacer(" ", "", "\n", "")
	b, err := hex.DecodeString(replacer.Replace(s))
	if err != nil {
		panic(err)
	}
	return b
}

func handler(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	accept := r.Header.Get("Accept")
	const mimeType = "application/dns-udpwireformat"

	if contentType != mimeType {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		io.WriteString(w, "invalid value for header \"Content-Type\"")
		return
	}
	if accept != mimeType {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		io.WriteString(w, "invalid value for header \"Accept\"")
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Write(hexDecode(response))
}

func TestExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	msg := dns.Msg{}
	if err := msg.Unpack(hexDecode(request)); err != nil {
		t.Fatal(err)
	}

	client := NewClient(10 * time.Second)
	reply, _, err := client.Exchange(&msg, srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	want := `;; opcode: QUERY, status: NOERROR, id: 0
;; flags: qr rd ra; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 0

;; QUESTION SECTION:
;www.example.com.	IN	 A

;; ANSWER SECTION:
www.example.com.	128	IN	A	192.0.2.1
`
	if got := reply.String(); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
