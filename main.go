package main

import (
	"database/sql"
	"fmt"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/miekg/dns"
	"gopkg.in/ini.v1"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
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

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}
func checkErrWarmly(err error) {
	if err != nil {
		log.Println(err)
	}
}

func LoadConfig() {
	fmt.Println("[*] Loading config")
	var config_file = "config.ini"
	if _, err := os.Stat(config_file); os.IsNotExist(err) {
		fmt.Println("[*] Config file not found, creating...")
		src, err := os.Open("config.default.ini")
		defer func(src *os.File) {
			checkErr(src.Close())
		}(src)
		dst, err := os.OpenFile(config_file, os.O_WRONLY|os.O_CREATE, 0644)
		checkErr(err)
		defer func(dst *os.File) {
			checkErr(dst.Close())
		}(dst)
		_, _ = io.Copy(dst, src)
		fmt.Println("[*] Created config.ini")
	}
	config_ptr, err := ini.Load(config_file)
	if err != nil {
		log.Fatalln(err)
	}

	// sub func
	getConfig := func(str string) string {
		config_section, err := config_ptr.GetSection("config")
		if err != nil {
			log.Println("Read section [config] failed, please check your config.ini")
		}
		value, err := config_section.GetKey(str)
		if err != nil {
			log.Fatalf("读取%s失败，请设置！\n", str)
		}
		return value.String()
	}
	config.ReturnIP = getConfig("return_ip")
	config.DbPath = getConfig("db_file")
	config.ListenHttp = getConfig("listen_http")
	config.ListenDNS = getConfig("listen_dns")
	fmt.Println("[*] Config loaded")
}

func saveDatabase(record DNS) bool {
	_, err := db.Exec("INSERT INTO `dnslog` (`domain`, `type`, `resp`, `src`, `created_at`) VALUES (?, ?, ?, ?, ?)", &record.Domain, &record.Type, &record.Resp, &record.Src, &record.Created)
	checkErrWarmly(err)
	fmt.Printf("[+] REQ [%s] FROM [%s] RESP [%s]\n", record.Domain, record.Src, record.Resp)
	return true
}

func (this *handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		domain := msg.Question[0].Name
		if true {
			var record DNS
			record.Domain = domain
			record.Type = "A"
			record.Resp = config.ReturnIP
			record.Src = w.RemoteAddr().String()
			record.Created = time.Now().Local()
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
	check()
	go httpServer()
	fmt.Println("[+] Started!")
	fmt.Printf("[+] DNS Interface: %s\n", config.ListenDNS)
	srv := &dns.Server{Addr: config.ListenDNS, Net: "udp"}
	srv.Handler = &handler{}
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("DNS server start failed: %s.\nTry `sudo`.\n", err.Error())
		os.Exit(0)
	}
}

func check() {
	fmt.Println("[*] Database checking")
	var err error
	db, err = sql.Open("sqlite3", config.DbPath)
	checkErr(err)
	err = db.Ping()
	checkErr(err)
	exec, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='dnslog';")
	checkErr(err)
	defer func(exec *sql.Rows) {
		checkErr(exec.Close())
	}(exec)
	if !exec.Next() {
		fmt.Println("[*] Database initializing...")
		initSql := "create table dnslog(id integer constraint dnslog_pk primary key autoincrement, domain text, type text, resp text, src text, created_at text);"
		_, err := db.Exec(initSql)
		checkErr(err)
		initSql = "create index dnslog_domain_index on dnslog (domain);"
		_, err = db.Exec(initSql)
		checkErr(err)
		fmt.Println("[*] Database initialized.")
	}
	fmt.Println("[*] Database checking done.")
}

func httpServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/api/latest", func(c *gin.Context) {
		rows, err := db.Query("SELECT `id`, `domain`, `type`, `resp`, `src`, datetime(created_at) FROM dnslog ORDER BY `id` DESC LIMIT 10")
		checkErrWarmly(err)
		if err != nil {
			log.Fatal(err)
		}
		defer func(rows *sql.Rows) {
			checkErrWarmly(rows.Close())
		}(rows)
		logs := make([]DNS, 0)
		for rows.Next() {
			var d DNS
			var timeCreated string
			err = rows.Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &timeCreated)
			d.Created, _ = time.Parse(TimeLayout, timeCreated)
			logs = append(logs, d)
		}
		c.JSON(http.StatusOK, gin.H{
			"data": logs,
		})
	})
	r.POST("/api/validate", func(c *gin.Context) {
		var query Query
		if c.ShouldBindJSON(&query) == nil {
			var d DNS
			query.Domain += "."
			m, _ := time.ParseDuration("-5m")
			var timeCreated string
			err := db.QueryRow("SELECT `id`, `domain`,`type`,`resp`,`src`,datetime(created_at) FROM dnslog WHERE `domain` = ? and `created_at` >= ? LIMIT 1", query.Domain, time.Now().Add(m)).Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &timeCreated)
			d.Created, _ = time.Parse(TimeLayout, timeCreated)
			if err != nil {
				checkErrWarmly(err)
				c.JSON(http.StatusNoContent, gin.H{
					"msg": "No record found in the last 5 minutes",
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"data": d,
			})
			return
		}
		c.JSON(http.StatusNotAcceptable, gin.H{
			"status": "0",
			"msg":    "Wrong request format",
		})
	})
	listenAddr := fmt.Sprintf(config.ListenHttp)
	fmt.Printf("[*] HTTP API: %s\n", listenAddr)
	_ = r.Run(listenAddr)
}
