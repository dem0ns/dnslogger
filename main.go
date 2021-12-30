package main

import (
	"database/sql"
	"fmt"
	"github.com/gin-gonic/gin"
	_ "github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/miekg/dns"
	"gopkg.in/ini.v1"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

type DNS struct {
	Id      int
	Domain  string
	Type    string
	Resp    string
	Src     string
	Created time.Time
}

type Config struct {
	Conn      string
	DefaultIp string
}

type Query struct {
	Domain string `form:"Domain"`
}

var config Config

func getConfig(str string) string {
	var err error
	var filepath = "config.ini"
	config, err := ini.Load(filepath)
	if err != nil {
		log.Fatalln("请配置config.ini文件")
	}
	config_section, err := config.GetSection("DNSLog_config")
	if err != nil {
		log.Println("读取section失败")
	}
	value, err := config_section.GetKey(str)
	if err != nil {
		log.Fatalln("读取" + str + "失败，请设置！")
	}
	return value.String()
}

func loadConfig() {
	fmt.Println("[*] Loading config...")
	config.Conn = getConfig("conn")
	config.DefaultIp = getConfig("default_ip")
	fmt.Println("[*] Done.")
}

func saveDatabase(record DNS) bool {
	Db, err := sql.Open("mysql", config.Conn)
	if err != nil {
		log.Fatalln(err)
	}
	defer Db.Close()
	_, err = Db.Exec("INSERT INTO `record` (`domain`, `type`, `resp`, `src`, `created_at`) VALUES (?, ?, ?, ?, ?)", &record.Domain, &record.Type, &record.Resp, &record.Src, &record.Created)
	if err != nil {
		log.Println(err)
	}
	fmt.Println("[+] " + record.Src + " asked " + record.Domain + " & response " + record.Resp)
	return true
}

type handler struct{}

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
			record.Resp = config.DefaultIp
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
	fmt.Println("[+] Welcome to DNSLogger.")
	fmt.Println("[+] Starting...")
	loadConfig()
	check()
	go httpServer()
	fmt.Println("[+] Server Started")
	fmt.Println("[+] GitHub: https://github.com/dem0ns/dnslogger")
	srv := &dns.Server{Addr: ":" + strconv.Itoa(53), Net: "udp"}
	srv.Handler = &handler{}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	}
}

func check() {
	fmt.Println("[*] Testing SQL connection...")
	Db, err := sql.Open("mysql", config.Conn)
	if err != nil {
		log.Fatalln(err)
	}
	err = Db.Ping()
	if err != nil {
		log.Fatalln(err)
	}
	Db.Close()
	fmt.Println("[*] Done.")
}

func httpServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	Db, err := sql.Open("mysql", config.Conn)
	if err != nil {
		log.Fatalln(err)
	}
	r.GET("/api/latest", func(c *gin.Context) {
		rows, err := Db.Query("SELECT `id`, `domain`, `type`, `resp`, `src`, `created_at` FROM record ORDER BY `id` DESC LIMIT 10")
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()
		logs := make([]DNS, 0)
		for rows.Next() {
			var d DNS
			_ = rows.Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &d.Created)
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
			query.Domain = query.Domain + "."
			m, _ := time.ParseDuration("-5m")
			err := Db.QueryRow("SELECT `id`, `domain`,`type`,`resp`,`src`,`created_at` FROM record WHERE `domain` = ? and `created_at` >= ? LIMIT 1", query.Domain, time.Now().Add(m)).Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &d.Created)
			if err != nil {
				c.JSON(http.StatusNoContent, gin.H{
					"msg": "No record(s) within 5 minute.",
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
			"msg":    "You looks like a 🐖.",
		})
	})
	_ = r.Run("127.0.0.1:1965")
}
