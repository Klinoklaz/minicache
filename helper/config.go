package helper

import (
	"encoding/json"
	"os"
	"time"
)

const (
	ModePass  byte = 'P'
	ModeBlock byte = 'B'
	ModeCache byte = 'C'
	ModeQueue byte = 'Q'
)

type config struct {
	LocalAddr        string        `json:"local_addr"`  // Local listening address
	TargetAddr       string        `json:"target_addr"` // Proxy target
	LogFile          string        `json:"log_file"`    // Specify a log destination
	LogLevel         int           // Specify a log level: debug|info|warning|error
	CacheUnique      bool          `json:"cache_unique"` // Deduplicate if different URLs return same response?
	CacheMobile      bool          `json:"cache_mobile"` // Detect mobile UA and cache the responses separately?
	CacheSize        int           `json:"cache_size"`   // Max cache size in bytes, default 1 GB
	NonGetMode       byte          // How to deal with non-GET requests: pass|block|cache|queue
	QueueCap         int           `json:"queue_capacity"` // Queue at most this number of requests for `non_get_mode=queue`. Otherwise has no effect
	DequeueRate      float32       `json:"dequeue_rate"`   // Dequeue and forward this number of queued requests per second when `non_get_mode=queue`
	LruTime          time.Duration // track access count within this time period (minutes) for each cache entry
	ProtectionExpire time.Duration // Fresh requests will go stale and fall into LRU list after this much of time (minutes)
	// TODO: support cache TTL, manual cache deleting
}

var Config config = config{
	LocalAddr:        ":80",
	LogLevel:         LogWarn,
	CacheSize:        1 << 30,
	NonGetMode:       ModePass,
	LruTime:          time.Duration(30) * time.Minute,
	ProtectionExpire: time.Duration(30) * time.Minute,
}

func LoadConfFile(file string) {
	data, err := os.ReadFile(file)
	if err != nil {
		Log(LogWarn, "can't read config file %s, default config values will be used. #%s", file, err)
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
		Log(LogWarn, "invalid config file %s, default config values will be used. #%s", file, err)
		return
	}

	switch jsonData.LL {
	case "debug":
		Config.LogLevel = LogDebug
	case "info":
		Config.LogLevel = LogInfo
	case "warning":
		Config.LogLevel = LogWarn
	case "error":
		Config.LogLevel = LogErr
	}

	if Config.LogFile != "" {
		setLogFile(Config.LogFile)
	}

	switch jsonData.NG {
	case "pass":
		Config.NonGetMode = ModePass
	case "block":
		Config.NonGetMode = ModeBlock
	case "cache":
		Config.NonGetMode = ModeCache
	case "queue":
		Config.NonGetMode = ModeQueue
		proxyQueue = make(chan bool)
		go dequeue()
	}

	if jsonData.LT > 0 {
		Config.LruTime = time.Duration(jsonData.LT) * time.Minute
	}
	if jsonData.EX > 0 {
		Config.ProtectionExpire = time.Duration(jsonData.EX) * time.Minute
	}

	Log(LogInfo, "config file loaded, current conf values: %+v", Config)
}
