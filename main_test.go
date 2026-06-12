package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	testDNSAddr  = "127.0.0.1:10053"
	testHTTPAddr = "127.0.0.1:18053"
	testDBPath   = "test_dnslog.db"
)

func TestMain(m *testing.M) {
	os.Remove(testDBPath)

	InitDB(testDBPath)
	UpdateConfigMap(map[string]string{
		"return_ip":   "127.0.0.1",
		"listen_dns":  testDNSAddr,
		"listen_http": testHTTPAddr,
	})

	// Add a wildcard rule so *.example.com gets intercepted and logged
	CreateFilterRule(&FilterRule{
		Name:      "test-allow",
		Pattern:   "*.example.com",
		MatchType: "wildcard",
		IP:        "127.0.0.1",
		Action:    "allow",
		Enabled:   true,
	})

	go StartHTTPServer(testHTTPAddr)
	srv := &dns.Server{Addr: testDNSAddr, Net: "udp", Handler: &fullHandler{}}
	go srv.ListenAndServe()

	for i := 0; i < 50; i++ {
		resp, err := http.Get(fmt.Sprintf("http://%s/api/config", testHTTPAddr))
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	code := m.Run()
	srv.Shutdown()
	if db != nil {
		db.Close()
	}
	os.Remove(testDBPath)
	os.Exit(code)
}

func TestDNSQuery(t *testing.T) {
	c := dns.Client{}
	m := dns.Msg{}
	m.SetQuestion("test.example.com.", dns.TypeA)
	r, _, err := c.Exchange(&m, testDNSAddr)
	if err != nil {
		t.Fatalf("DNS exchange failed: %v", err)
	}
	if r == nil || len(r.Answer) == 0 {
		t.Fatal("No DNS answer received")
	}
	for _, ans := range r.Answer {
		if a, ok := ans.(*dns.A); ok {
			if a.A.String() != "127.0.0.1" {
				t.Errorf("DNS response IP: got %s, want 127.0.0.1", a.A.String())
			}
		}
	}
}

func TestAPIConfig(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://%s/api/config", testHTTPAddr))
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Status: got %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Fatal("Invalid config response")
	}
	if data["return_ip"] != "127.0.0.1" {
		t.Errorf("return_ip: got %v, want 127.0.0.1", data["return_ip"])
	}
}

func TestAPILogs(t *testing.T) {
	// Query matches *.example.com rule → will be logged
	c := dns.Client{}
	m := dns.Msg{}
	m.SetQuestion("logtest.example.com.", dns.TypeA)
	c.Exchange(&m, testDNSAddr)
	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/logs", testHTTPAddr))
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Status: got %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	total := int(result["total"].(float64))
	if total < 1 {
		t.Errorf("Expected at least 1 log, got %d", total)
	}
}

func TestAPIValidate(t *testing.T) {
	// Query matches *.example.com rule → will be logged
	c := dns.Client{}
	m := dns.Msg{}
	m.SetQuestion("validatetest.example.com.", dns.TypeA)
	c.Exchange(&m, testDNSAddr)
	time.Sleep(200 * time.Millisecond)

	body, _ := json.Marshal(map[string]interface{}{"domain": "validatetest.example.com", "minutes": 5})
	resp, err := http.Post(fmt.Sprintf("http://%s/api/validate", testHTTPAddr), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	found, ok := result["found"].(bool)
	if !ok || !found {
		t.Errorf("Expected domain to be found, got %v", result)
	}
}

func TestAPIFilters(t *testing.T) {
	base := fmt.Sprintf("http://%s/api/filters", testHTTPAddr)

	body, _ := json.Marshal(map[string]interface{}{
		"name": "test-rule", "pattern": "*.test.com",
		"match_type": "wildcard", "action": "allow", "enabled": true,
	})
	resp, err := http.Post(base, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Create filter failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Create: got %d, want 200", resp.StatusCode)
	}

	resp, err = http.Get(base)
	if err != nil {
		t.Fatalf("List filters failed: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	data := result["data"].([]interface{})
	if len(data) < 2 { // 1 from TestMain + 1 just created
		t.Errorf("Expected at least 2 filter rules, got %d", len(data))
	}
}

func TestAPIUpdateConfig(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"return_ip": "10.0.0.1"})
	req, _ := http.NewRequest("PUT", fmt.Sprintf("http://%s/api/config", testHTTPAddr), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Update config failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Update: got %d, want 200", resp.StatusCode)
	}

	resp, _ = http.Get(fmt.Sprintf("http://%s/api/config", testHTTPAddr))
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	data := result["data"].(map[string]interface{})
	if data["return_ip"] != "10.0.0.1" {
		t.Errorf("return_ip after update: got %v, want 10.0.0.1", data["return_ip"])
	}

	// Restore
	body, _ = json.Marshal(map[string]string{"return_ip": "127.0.0.1"})
	req, _ = http.NewRequest("PUT", fmt.Sprintf("http://%s/api/config", testHTTPAddr), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(req)
}
