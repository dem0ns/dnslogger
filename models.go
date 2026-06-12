package main

import (
	"database/sql"
	"time"
)

const TimeLayout = "2006-01-02 15:04:05"

var db *sql.DB
var config map[string]string

type DNSLog struct {
	Id      int       `json:"id"`
	Domain  string    `json:"domain"`
	Type    string    `json:"type"`
	Resp    string    `json:"resp"`
	Src     string    `json:"src"`
	Created time.Time `json:"created"`
}

type ConfigItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type FilterRule struct {
	Id          int       `json:"id"`
	Name        string    `json:"name"`
	Pattern     string    `json:"pattern"`
	MatchType   string    `json:"match_type"`
	IP          string    `json:"ip"`
	Action      string    `json:"action"`
	Enabled     bool      `json:"enabled"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
