package main

import (
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// --- Pattern Matching ---

func matchDomain(domain, pattern, matchType string) bool {
	domain = strings.ToLower(domain)
	pattern = strings.ToLower(pattern)

	switch matchType {
	case "exact":
		return domain == pattern
	case "contains":
		return strings.Contains(domain, pattern)
	case "wildcard":
		return matchWildcard(domain, pattern)
	case "regex":
		matched, err := regexp.MatchString(pattern, domain)
		if err != nil {
			log.Printf("[-] Invalid regex pattern '%s': %v", pattern, err)
			return false
		}
		return matched
	default:
		return false
	}
}

func matchWildcard(domain, pattern string) bool {
	// Convert glob pattern to regex: * -> [^.]*, ? -> [^.]
	regexStr := "^"
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			regexStr += "[^.]*"
		case '?':
			regexStr += "[^.]"
		case '.':
			regexStr += "\\."
		default:
			regexStr += regexp.QuoteMeta(string(pattern[i]))
		}
	}
	regexStr += "$"

	matched, err := regexp.MatchString(regexStr, domain)
	if err != nil {
		return false
	}
	return matched
}

// --- Full Mode DNS Handler ---

type fullHandler struct{}

func (h *fullHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)

	if len(r.Question) == 0 {
		_ = w.WriteMsg(&msg)
		return
	}

	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		domain := r.Question[0].Name
		domainClean := strings.TrimSuffix(domain, ".")

		var respIP string
		respIP, matched := resolveDomain(domainClean)

		if matched {
			// Rule matched → return rule IP
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0},
				A:   net.ParseIP(respIP),
			})
		} else {
			// No rule matched → forward to upstream DNS
			upstreamMsg := upstreamResolveMsg(domainClean)
			if upstreamMsg != nil && len(upstreamMsg.Answer) > 0 {
				msg.Answer = upstreamMsg.Answer
				// Extract IP from upstream answer for logging
				for _, ans := range upstreamMsg.Answer {
					if a, ok := ans.(*dns.A); ok {
						respIP = a.A.String()
						break
					}
				}
			}
			if respIP == "" {
				respIP = config["return_ip"]
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0},
					A:   net.ParseIP(respIP),
				})
			}
		}

		// Log every DNS query
		InsertDNSLog(domainClean, "A", respIP, w.RemoteAddr().String())
		fmt.Printf("[+] [%s] FROM [%s] RESP [%s]\n", domainClean, w.RemoteAddr().String(), respIP)
		BroadcastLog(DNSLog{
			Domain:  domainClean,
			Type:    "A",
			Resp:    respIP,
			Src:     w.RemoteAddr().String(),
			Created: time.Now(),
		})
	}

	_ = w.WriteMsg(&msg)
}

// resolveDomain checks filter rules and returns (ip, true) if matched, ("", false) if not.
func resolveDomain(domain string) (string, bool) {
	rules, err := GetEnabledFilterRules()
	if err != nil || len(rules) == 0 {
		return "", false
	}

	// Check block rules first
	for _, rule := range rules {
		if rule.Action == "block" && matchDomain(domain, rule.Pattern, rule.MatchType) {
			if rule.IP != "" {
				return rule.IP, true // Blocked with specific IP
			}
			return config["return_ip"], true // Blocked → return default IP
		}
	}

	// Check allow/redirect rules
	for _, rule := range rules {
		if rule.Action != "allow" {
			continue
		}
		if matchDomain(domain, rule.Pattern, rule.MatchType) {
			if rule.IP != "" {
				return rule.IP, true // Rule has specific IP
			}
			return config["return_ip"], true // Rule matched but no specific IP
		}
	}

	return "", false
}

// --- Simple Mode DNS Handler ---

type simpleHandler struct {
	returnIP string
}

var defaultSimpleRules = map[string]string{
	"www.linkedin.com":          "12.12.12.12",
	"open.douyin.com":           "12.12.12.12",
	"example.com":               "12.12.12.12",
	"e.juliangyinqing.com":      "12.12.12.12",
	"e.oceanengine.com":         "12.12.12.12",
	"sec.alipay.com":            "12.12.12.12",
	"accounts.feishu.cn":        "12.12.12.12",
	"larksuite.com":             "12.12.12.12",
	"www.example.com":           "12.12.12.12",
	"developer.open-douyin.com": "12.12.12.12",
}

func (h *simpleHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)

	if len(r.Question) == 0 {
		_ = w.WriteMsg(&msg)
		return
	}

	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		domain := r.Question[0].Name
		domainClean := strings.TrimSuffix(domain, ".")

		ip := h.resolve(domainClean)
		fmt.Printf("[+] %s → %s\n", domainClean, ip)

		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0},
			A:   net.ParseIP(ip),
		})

	case dns.TypeCNAME:
		msg.Authoritative = true
		domain := r.Question[0].Name
		domainClean := strings.TrimSuffix(domain, ".")
		fmt.Printf("[+] CNAME %s → example.com\n", domainClean)

		msg.Answer = append(msg.Answer, &dns.CNAME{
			Hdr:    dns.RR_Header{Name: domain, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 0},
			Target: "example.com.",
		})

	default:
		msg.SetRcode(r, dns.RcodeNotImplemented)
	}

	_ = w.WriteMsg(&msg)
}

func (h *simpleHandler) resolve(domain string) string {
	// Check default rules
	if ip, ok := defaultSimpleRules[domain]; ok {
		return ip
	}
	// Check wildcard: *.www suffix (from original dns_intercept)
	if strings.HasSuffix(domain, ".www") {
		return h.returnIP
	}
	// Forward to upstream
	return upstreamResolve(domain)
}

// --- Upstream DNS Resolution ---

// getUpstreamServer returns the upstream DNS server address.
// Uses config["upstream_dns"] if set, otherwise reads from /etc/resolv.conf.
func getUpstreamServer() string {
	if custom := config["upstream_dns"]; custom != "" {
		// If no port specified, add :53
		if _, _, err := net.SplitHostPort(custom); err != nil {
			return net.JoinHostPort(custom, "53")
		}
		return custom
	}
	conf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil || len(conf.Servers) == 0 {
		return ""
	}
	return net.JoinHostPort(conf.Servers[0], conf.Port)
}

// upstreamResolveMsg returns the full upstream DNS response message.
func upstreamResolveMsg(domain string) *dns.Msg {
	server := getUpstreamServer()
	if server == "" {
		return nil
	}

	c := new(dns.Client)
	c.Timeout = 3 * time.Second
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	m.RecursionDesired = true

	r, _, err := c.Exchange(m, server)
	if err != nil || r == nil {
		return nil
	}
	return r
}

func upstreamResolve(domain string) string {
	server := getUpstreamServer()
	if server == "" {
		return "0.0.0.0"
	}

	c := new(dns.Client)
	c.Timeout = 3 * time.Second
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	m.RecursionDesired = true

	r, _, _ := c.Exchange(m, server)
	if r == nil {
		return "0.0.0.0"
	}
	if r.Rcode != dns.RcodeSuccess {
		return "0.0.0.0"
	}
	for _, ans := range r.Answer {
		if a, ok := ans.(*dns.A); ok {
			return a.A.String()
		}
	}
	return "0.0.0.0"
}
