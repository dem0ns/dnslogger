package main

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestCheck(t *testing.T) {
	t.Run("check", func(t *testing.T) {
		fmt.Println("ok")
	})
}

func TestMain(m *testing.M) {
	// Use high ports to avoid needing root privileges in CI
	os.WriteFile("config.ini", []byte(`[config]
db_file = dnslog.db
return_ip = 127.0.0.1
listen_dns = 127.0.0.1:10053
listen_http = 127.0.0.1:12020
domain = log.dnslogger.local
`), 0644)

	go main()

	// Wait for HTTP server to be ready
	apiBase := "http://127.0.0.1:12020"
	for i := 0; i < 50; i++ {
		resp, err := http.Get(fmt.Sprintf("%s/api/latest", apiBase))
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	os.Exit(m.Run())
}

func TestDNS(t *testing.T) {
	t.Run("req", func(t *testing.T) {
		ns := "127.0.0.1:10053"
		c := dns.Client{}
		m := dns.Msg{}
		m.SetQuestion("dnslogger.local.", dns.TypeA)
		r, _, err := c.Exchange(&m, ns)
		if err != nil {
			t.Fatalf("DNS exchange failed: %v", err)
		}
		if r == nil || len(r.Answer) == 0 {
			t.Fatal("No DNS answer received")
		}
		for _, ans := range r.Answer {
			if a, ok := ans.(*dns.A); ok {
				if a.A.String() != config.ReturnIP {
					t.Errorf("DNS response IP mismatch: got %s, want %s", a.A.String(), config.ReturnIP)
				}
			}
		}
	})

	apiBase := "http://127.0.0.1:12020"

	t.Run("queryLatest", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/latest", apiBase))
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("API status code: got %d, want 200", resp.StatusCode)
		}
	})

	t.Run("validate", func(t *testing.T) {
		resp, err := http.Post(fmt.Sprintf("%s/api/validate", apiBase), "application/json",
			nil)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		defer resp.Body.Close()
		// No body sent, should return 406
		if resp.StatusCode != http.StatusNotAcceptable {
			t.Errorf("API validate status code: got %d, want %d", resp.StatusCode, http.StatusNotAcceptable)
		}
	})
}
