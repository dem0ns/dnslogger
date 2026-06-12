package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"nhooyr.io/websocket"
)

//go:embed static/index.html
var staticFS embed.FS

// --- WebSocket Hub ---

type wsHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

var hub = &wsHub{clients: make(map[*websocket.Conn]bool)}

func (h *wsHub) add(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()
}

func (h *wsHub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
}

// BroadcastLog sends a log entry to all connected WebSocket clients.
func BroadcastLog(entry DNSLog) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for conn := range hub.clients {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn.Write(ctx, websocket.MessageText, data)
		cancel()
	}
}

func StartHTTPServer(addr string) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Serve embedded HTML
	r.GET("/", func(c *gin.Context) {
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			c.String(500, "Failed to load page")
			return
		}
		c.Data(200, "text/html; charset=utf-8", data)
	})

	// WebSocket endpoint for live log streaming
	r.GET("/ws/logs", func(c *gin.Context) {
		conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		hub.add(conn)
		defer hub.remove(conn)

		// Keep connection alive; read messages to detect disconnect
		for {
			_, _, err := conn.Read(c.Request.Context())
			if err != nil {
				break
			}
		}
	})

	// --- Logs API ---
	r.GET("/api/logs", func(c *gin.Context) {
		domain := c.Query("domain")
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limit <= 0 || limit > 500 {
			limit = 50
		}

		logs, err := QueryDNSLogs(domain, limit, offset)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		count, _ := CountDNSLogs(domain)
		c.JSON(200, gin.H{"data": logs, "total": count})
	})

	r.GET("/api/logs/count", func(c *gin.Context) {
		domain := c.Query("domain")
		count, err := CountDNSLogs(domain)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"count": count})
	})

	r.POST("/api/validate", func(c *gin.Context) {
		var req struct {
			Domain  string `json:"domain"`
			Minutes int    `json:"minutes"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Domain == "" {
			c.JSON(400, gin.H{"error": "Invalid request, 'domain' is required"})
			return
		}
		if req.Minutes <= 0 {
			req.Minutes = 5
		}

		// Try exact match first, then with trailing dot
		record, err := ValidateDomain(req.Domain, req.Minutes)
		if err != nil {
			record, err = ValidateDomain(req.Domain+".", req.Minutes)
		}
		if err != nil {
			c.JSON(200, gin.H{"found": false, "msg": "No record found"})
			return
		}
		c.JSON(200, gin.H{"found": true, "data": record})
	})

	r.DELETE("/api/logs", func(c *gin.Context) {
		if err := ClearDNSLogs(); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"msg": "Logs cleared"})
	})

	// --- Config API ---
	r.GET("/api/config", func(c *gin.Context) {
		cfg := GetConfigMap()
		c.JSON(200, gin.H{"data": cfg})
	})

	r.PUT("/api/config", func(c *gin.Context) {
		var items map[string]string
		if err := c.ShouldBindJSON(&items); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request format"})
			return
		}
		if ip, ok := items["return_ip"]; ok {
			if net.ParseIP(ip) == nil {
				c.JSON(400, gin.H{"error": "Invalid return_ip address"})
				return
			}
		}
		if err := UpdateConfigMap(items); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"msg": "Config updated"})
	})

	// --- Filter Rules API ---
	r.GET("/api/filters", func(c *gin.Context) {
		rules, err := GetFilterRules()
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"data": rules})
	})

	r.POST("/api/filters", func(c *gin.Context) {
		var rule FilterRule
		if err := c.ShouldBindJSON(&rule); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request format"})
			return
		}
		if rule.Pattern == "" {
			c.JSON(400, gin.H{"error": "pattern is required"})
			return
		}
		if rule.Name == "" {
			rule.Name = rule.Pattern
		}
		validTypes := map[string]bool{"exact": true, "wildcard": true, "regex": true, "contains": true}
		if !validTypes[rule.MatchType] {
			c.JSON(400, gin.H{"error": "match_type must be one of: exact, wildcard, regex, contains"})
			return
		}
		if rule.Action != "allow" && rule.Action != "block" {
			c.JSON(400, gin.H{"error": "action must be 'allow' or 'block'"})
			return
		}
		if rule.MatchType == "regex" {
			if _, err := regexp.Compile(rule.Pattern); err != nil {
				c.JSON(400, gin.H{"error": "Invalid regex pattern: " + err.Error()})
				return
			}
		}

		if err := CreateFilterRule(&rule); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"data": rule})
	})

	r.PUT("/api/filters/:id", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid rule ID"})
			return
		}
		var rule FilterRule
		if err := c.ShouldBindJSON(&rule); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request format"})
			return
		}
		rule.Id = id
		if rule.Action != "allow" && rule.Action != "block" {
			c.JSON(400, gin.H{"error": "action must be 'allow' or 'block'"})
			return
		}
		if err := UpdateFilterRule(&rule); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"data": rule})
	})

	r.DELETE("/api/filters/:id", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid rule ID"})
			return
		}
		if err := DeleteFilterRule(id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"msg": "Rule deleted"})
	})

	fmt.Printf("[*] HTTP API: %s\n", addr)
	_ = r.Run(addr)
}
