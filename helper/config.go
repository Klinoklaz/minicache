package helper

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

type config struct {
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
	LruTime          time.Duration // track access count within this time period (minutes) for each cache entry
	ProtectionExpire time.Duration // Fresh requests will go stale and fall into LRU list after this much of time (minutes)
	// TODO: support cache TTL, manual cache deleting
}

var Config config = config{
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
		*config
		LL string `json:"log_level"`
		NG string `json:"non_get_mode"`
		LT int    `json:"lru_time"`
		EX int    `json:"protection_expire"`
	}{config: &Config}

	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		Log("", LOG_WARN)
		return
	}

	switch jsonData.LL {
	case "debug":
		Config.LogLevel = LOG_DEBUG
	case "info":
		Config.LogLevel = LOG_NOTICE
	case "warning":
		Config.LogLevel = LOG_WARN
	case "error":
		Config.LogLevel = LOG_ERR
	}

	switch jsonData.NG {
	case "pass":
		Config.NonGetMode = MODE_PASS
	case "block":
		Config.NonGetMode = MODE_BLOCK
	case "cache":
		Config.NonGetMode = MODE_CACHE
	case "queue":
		Config.NonGetMode = MODE_QUEUE
	}

	if jsonData.LT > 0 {
		Config.LruTime = time.Duration(jsonData.LT) * time.Minute
	}
	if jsonData.EX > 0 {
		Config.ProtectionExpire = time.Duration(jsonData.EX) * time.Minute
	}

	Log(fmt.Sprintf("Loaded config JSON: %v, current ConfGlobal: %v", jsonData, Config), LOG_NOTICE)
}
