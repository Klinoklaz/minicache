package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const (
	MODE_PASS  byte = 'P'
	MODE_BLOCK byte = 'B'
	MODE_CACHE byte = 'C'
	MODE_QUEUE byte = 'Q'
)

type Config struct {
	LocalAddr        string        `json:"local_addr"`  // Local listening address
	TargetAddr       string        `json:"target_addr"` // Proxy target
	LogFile          string        `json:"log_file"`    // Specify a log destination
	LogLevel         byte          // Specify a log level: debug|info|warning|error
	CacheUnique      bool          `json:"cache_unique"` // Deduplicate if different URLs return same response?
	CacheMobile      bool          `json:"cache_mobile"` // Detect mobile UA and cache the responses separately?
	CacheSize        int           `json:"cache_size"`   // Max cache size in bytes, default 1 GB
	NonGetMode       byte          // How to deal with non-GET requests: pass|block|cache|queue
	QueueLength      int           `json:"queue_length"` // Queue at most this number of requests for `non_get_mode=queue`. Otherwise has no effect
	QueueSize        int           `json:"queue_size"`   // Max queue size in bytes for `non_get_mode=queue`, default 1 MB. Otherwise has no effect
	DequeueRate      int           `json:"dequeue_rate"` // Dequeue and forward this number of queued requests per second when `non_get_mode=queue`
	LruTime          time.Duration // track access count in this time period (minutes) for each entry of LRU list
	ProtectionExpire time.Duration // Fresh requests will go stale and fall into LRU list after this much of time (minutes)
	// TODO support cache expiration time
}

var ConfGlobal Config = Config{
	LocalAddr:        ":80",
	LogLevel:         LOG_WARN,
	CacheSize:        1 << 30,
	NonGetMode:       MODE_PASS,
	QueueSize:        1 << 20,
	LruTime:          time.Duration(30) * time.Minute,
	ProtectionExpire: time.Duration(30) * time.Minute,
}

func LoadConfFile(file string) {
	data, err := os.ReadFile(file)
	if err != nil {
		Log("", LOG_WARN)
		return
	}

	jsonData := struct {
		*Config
		LL string `json:"log_level"`
		NG string `json:"non_get_mode"`
		LT int    `json:"lru_time"`
		EX int    `json:"protection_expire"`
	}{Config: &ConfGlobal}

	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		Log("", LOG_WARN)
		return
	}

	switch jsonData.LL {
	case "debug":
		ConfGlobal.LogLevel = LOG_DEBUG
	case "info":
		ConfGlobal.LogLevel = LOG_NOTICE
	case "warning":
		ConfGlobal.LogLevel = LOG_WARN
	case "error":
		ConfGlobal.LogLevel = LOG_ERR
	}

	switch jsonData.NG {
	case "pass":
		ConfGlobal.NonGetMode = MODE_PASS
	case "block":
		ConfGlobal.NonGetMode = MODE_BLOCK
	case "cache":
		ConfGlobal.NonGetMode = MODE_CACHE
	case "queue":
		ConfGlobal.NonGetMode = MODE_QUEUE
	}

	if jsonData.LT > 0 {
		ConfGlobal.LruTime = time.Duration(jsonData.LT) * time.Minute
	}
	if jsonData.EX > 0 {
		ConfGlobal.ProtectionExpire = time.Duration(jsonData.EX) * time.Minute
	}

	Log(fmt.Sprintf("Loaded config JSON: %v, current ConfGlobal: %v", jsonData, ConfGlobal), LOG_NOTICE)
}
