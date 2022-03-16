package main

import (
	"fmt"
	"github.com/miekg/dns"
	"net/http"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	go main()
	time.Sleep(3 * time.Second)
	_ = m.Run()
}

func TestDNS(t *testing.T) {
	t.Run("req", func(t *testing.T) {
		ns := "localhost:53"
		c := dns.Client{}
		m := dns.Msg{}
		m.SetQuestion("dnslogger.local.", dns.TypeA)
		r, _, err := c.Exchange(&m, ns)
		checkErr(err)
		for _, ans := range r.Answer {
			if ans.(*dns.A).A.String() != config.ReturnIP {
				t.Errorf("DNS解析错误.\n")
			}
		}
	})
	ApiBase := fmt.Sprintf("http://%s", config.ListenHttp)
	t.Run("queryLatest", func(t *testing.T) {
		reqest, err := http.NewRequest("GET", fmt.Sprintf("%s/api/latest", ApiBase), nil)
		if err != nil {
			panic(err)
		}
		response, _ := (&http.Client{}).Do(reqest)
		if response.StatusCode != 200 {
			t.Errorf("API status code异常. %d\n", response.StatusCode)
		}
	})
}
