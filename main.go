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
	"strconv"
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
	DefaultIp string
	DbPath    string
	HttpPort  uint16
}

type Query struct {
	Domain string `form:"Domain"`
}

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

func getConfig(str string) string {
	var err error
	var config_file = "config.ini"
	if _, err := os.Stat(config_file); os.IsNotExist(err) {
		fmt.Println("[*] 配置文件不存在")
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
		fmt.Println("[*] 已创建配置文件")
	}
	config, err := ini.Load(config_file)
	if err != nil {
		log.Fatalln(err)
	}
	config_section, err := config.GetSection("config")
	if err != nil {
		log.Println("读取section失败")
	}
	value, err := config_section.GetKey(str)
	if err != nil {
		log.Fatalln("读取" + str + "失败，请设置！")
	}
	return value.String()
}

func LoadConfig() {
	fmt.Println("[*] 加载配置文件...")
	config.DefaultIp = getConfig("default_ip")
	config.DbPath = getConfig("db_file")
	port, err := strconv.ParseInt(getConfig("http_port"), 10, 16)
	checkErr(err)
	config.HttpPort = uint16(port)
	fmt.Printf("[*] HTTP API端口: %d\n", port)
	fmt.Println("[*] 配置文件加载完毕")
}

func saveDatabase(record DNS) bool {
	_, err := db.Exec("INSERT INTO `dnslog` (`domain`, `type`, `resp`, `src`, `created_at`) VALUES (?, ?, ?, ?, ?)", &record.Domain, &record.Type, &record.Resp, &record.Src, &record.Created)
	checkErrWarmly(err)
	fmt.Printf("[+] REQ [%s] FROM [%s] RESP [%s]\n", record.Domain, record.Src, record.Resp)
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
	fmt.Println("[+] Hello from DNSLogger")
	fmt.Println("[+] Starting...")
	LoadConfig()
	var err error
	db, err = sql.Open("sqlite3", config.DbPath)
	checkErr(err)
	defer func(db *sql.DB) {
		err := db.Close()
		checkErr(err)
	}(db)
	err = db.Ping()
	checkErr(err)
	check()
	go httpServer()
	fmt.Println("[+] Server Started!")
	listenAddr := fmt.Sprintf(":%s", strconv.Itoa(53))
	srv := &dns.Server{Addr: listenAddr, Net: "udp"}
	srv.Handler = &handler{}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	}
}

func check() {
	fmt.Println("[*] 数据库检查...")
	exec, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='dnslog';")
	checkErr(err)
	defer func(exec *sql.Rows) {
		checkErr(exec.Close())
	}(exec)
	if !exec.Next() {
		fmt.Println("[*] 数据库初始化中")
		initSql := "create table dnslog(id integer constraint dnslog_pk primary key autoincrement, domain text, type text, resp text, src text, created_at text);"
		_, err := db.Exec(initSql)
		checkErr(err)
		initSql = "create index dnslog_domain_index on dnslog (domain);"
		_, err = db.Exec(initSql)
		checkErr(err)
		fmt.Println("[*] 数据库初始化完毕")
	}
	fmt.Println("[*] 数据库检查完毕")
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
					"msg": "No record within 5 minute.",
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
			"msg":    "Wrong 🐖",
		})
	})
	listenAddr := fmt.Sprintf("127.0.0.1:%d", config.HttpPort)
	fmt.Printf("[*] HTTP API: %s\n", listenAddr)
	_ = r.Run(listenAddr)
}
