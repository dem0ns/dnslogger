package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/miekg/dns"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "simple" {
		runSimpleMode()
		return
	}
	runFullMode()
}

// --- Full Mode ---

func runFullMode() {
	fmt.Println("[+] DNSLogger Starting...")

	InitDB("dnslog.db")

	// Validate config
	if ip := config["return_ip"]; ip == "" {
		log.Fatal("return_ip not configured")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start DNS server
	srv := &dns.Server{Addr: config["listen_dns"], Net: "udp", Handler: &fullHandler{}}

	// Start HTTP server in background
	go StartHTTPServer(config["listen_http"])

	fmt.Println("[+] Started!")
	fmt.Printf("[+] DNS  Interface: %s\n", config["listen_dns"])
	fmt.Printf("[+] HTTP Interface: %s\n", config["listen_http"])

	go func() {
		<-ctx.Done()
		fmt.Println("\n[*] Shutting down...")
		if err := srv.Shutdown(); err != nil {
			log.Printf("[-] DNS server shutdown error: %v", err)
		}
		if db != nil {
			db.Close()
		}
	}()

	if err := srv.ListenAndServe(); err != nil {
		if ctx.Err() != nil {
			fmt.Println("[+] Stopped.")
			return
		}
		log.Fatalf("DNS server start failed: %v\nTry `sudo`.", err)
	}
}

// --- Simple Mode ---

func runSimpleMode() {
	fs := flag.NewFlagSet("simple", flag.ExitOnError)
	addr := fs.String("addr", ":53", "Listen address")
	ip := fs.String("ip", "12.12.12.12", "Return IP for matched domains")
	fs.Parse(os.Args[2:])

	fmt.Println("[+] DNSLogger Simple Mode")
	fmt.Printf("[+] Listen: %s\n", *addr)
	fmt.Printf("[+] Return IP: %s\n", *ip)
	fmt.Printf("[+] Built-in rules: %d domains\n", len(defaultSimpleRules))
	fmt.Println("[+] Unmatched domains will be forwarded to upstream DNS")
	fmt.Println()

	handler := &simpleHandler{returnIP: *ip}
	srv := &dns.Server{Addr: *addr, Net: "udp", Handler: handler}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		fmt.Println("\n[*] Shutting down...")
		srv.Shutdown()
	}()

	if err := srv.ListenAndServe(); err != nil {
		if ctx.Err() != nil {
			fmt.Println("[+] Stopped.")
			return
		}
		log.Fatalf("DNS server start failed: %v\nTry `sudo`.", err)
	}
}
