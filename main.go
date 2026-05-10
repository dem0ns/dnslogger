package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/miekg/dns"
	"gopkg.in/ini.v1"
)

var db *sql.DB

const TimeLayout = "2006-01-02 15:04:05"

type DNS struct {
	Id      int
	Domain  string
	Type    string
	Resp    string
	Src     string
	Created time.Time
}

type Config struct {
	ReturnIP   string
	DbPath     string
	ListenHttp string
	ListenDNS  string
}

type Query struct {
	Domain string `form:"Domain"`
}

type handler struct{}

var config Config

func LoadConfig() {
	fmt.Println("[*] Loading config")
	var configFile = "config.ini"
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("[*] Config file not found, creating...")
		src, err := os.Open("config.default.ini")
		if err != nil {
			log.Fatalf("Failed to open config.default.ini: %v", err)
		}
		defer src.Close()

		dst, err := os.OpenFile(configFile, os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			log.Fatalf("Failed to create config.ini: %v", err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			log.Fatalf("Failed to copy config file: %v", err)
		}
		fmt.Println("[*] Config file `config.ini` created.")
	}

	configPtr, err := ini.Load(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	getConfig := func(key string) string {
		section, err := configPtr.GetSection("config")
		if err != nil {
			log.Fatal("Failed to read section [config], please check your config.ini")
		}
		value, err := section.GetKey(key)
		if err != nil {
			log.Fatalf("Failed to read config key '%s', please set it in config.ini", key)
		}
		return value.String()
	}

	config.ReturnIP = getConfig("return_ip")
	if net.ParseIP(config.ReturnIP) == nil {
		log.Fatalf("Invalid return_ip '%s' in config.ini", config.ReturnIP)
	}
	config.DbPath = getConfig("db_file")
	config.ListenHttp = getConfig("listen_http")
	config.ListenDNS = getConfig("listen_dns")
	fmt.Println("[*] Config loaded")
}

func saveDatabase(record DNS) error {
	_, err := db.Exec("INSERT INTO `dnslog` (`domain`, `type`, `resp`, `src`, `created_at`) VALUES (?, ?, ?, ?, ?)",
		&record.Domain, &record.Type, &record.Resp, &record.Src, &record.Created)
	if err != nil {
		log.Printf("[-] Failed to save DNS record: %v", err)
		return err
	}
	fmt.Printf("[+] REQ [%s] FROM [%s] RESP [%s]\n", record.Domain, record.Src, record.Resp)
	return nil
}

func (h *handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		domain := msg.Question[0].Name
		if len(domain) > 1 {
			record := DNS{
				Domain:  domain[:len(domain)-1],
				Type:    "A",
				Resp:    config.ReturnIP,
				Src:     w.RemoteAddr().String(),
				Created: time.Now().Local(),
			}
			_ = saveDatabase(record)
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0},
				A:   net.ParseIP(record.Resp),
			})
		}
	}
	_ = w.WriteMsg(&msg)
}

func main() {
	fmt.Println("[+] DNSLogger Starting...")
	LoadConfig()
	checkDatabase()

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start HTTP server
	srv := &dns.Server{Addr: config.ListenDNS, Net: "udp", Handler: &handler{}}

	go httpServer()

	fmt.Println("[+] Started!")
	fmt.Printf("[+] DNS Interface: %s\n", config.ListenDNS)

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

func checkDatabase() {
	fmt.Println("[*] Database checking")
	var err error
	db, err = sql.Open("sqlite3", config.DbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	if err = db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='dnslog';")
	if err != nil {
		log.Fatalf("Failed to query database tables: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		fmt.Println("[*] Database initializing...")
		initSql := "CREATE TABLE dnslog(id INTEGER CONSTRAINT dnslog_pk PRIMARY KEY AUTOINCREMENT, domain TEXT, type TEXT, resp TEXT, src TEXT, created_at TEXT);"
		if _, err := db.Exec(initSql); err != nil {
			log.Fatalf("Failed to create table: %v", err)
		}
		initSql = "CREATE INDEX dnslog_domain_index ON dnslog (domain);"
		if _, err := db.Exec(initSql); err != nil {
			log.Fatalf("Failed to create index: %v", err)
		}
		fmt.Println("[*] Database initialized.")
	}
	fmt.Println("[*] Database checking done.")
}

func httpServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.GET("/api/latest", func(c *gin.Context) {
		rows, err := db.Query("SELECT `id`, `domain`, `type`, `resp`, `src`, datetime(created_at) FROM dnslog ORDER BY `id` DESC LIMIT 10")
		if err != nil {
			log.Printf("[-] Query latest failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "Database query failed"})
			return
		}
		defer rows.Close()

		logs := make([]DNS, 0)
		for rows.Next() {
			var d DNS
			var timeCreated string
			if err := rows.Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &timeCreated); err != nil {
				log.Printf("[-] Scan row failed: %v", err)
				continue
			}
			d.Created, _ = time.Parse(TimeLayout, timeCreated)
			logs = append(logs, d)
		}
		c.JSON(http.StatusOK, gin.H{"data": logs})
	})

	r.POST("/api/validate", func(c *gin.Context) {
		var query Query
		if c.ShouldBindJSON(&query) != nil {
			c.JSON(http.StatusNotAcceptable, gin.H{
				"status": "0",
				"msg":    "Wrong request format",
			})
			return
		}
		query.Domain += "."
		m, _ := time.ParseDuration("-5m")
		var d DNS
		var timeCreated string
		err := db.QueryRow("SELECT `id`, `domain`,`type`,`resp`,`src`,datetime(created_at) FROM dnslog WHERE `domain` = ? AND `created_at` >= ? LIMIT 1",
			query.Domain, time.Now().Add(m)).Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &timeCreated)
		if err != nil {
			c.JSON(http.StatusNoContent, gin.H{
				"msg": "No record found in the last 5 minutes",
			})
			return
		}
		d.Created, _ = time.Parse(TimeLayout, timeCreated)
		c.JSON(http.StatusOK, gin.H{"data": d})
	})

	fmt.Printf("[*] HTTP API: %s\n", config.ListenHttp)
	_ = r.Run(config.ListenHttp)
}
