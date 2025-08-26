package util

import (
	"encoding/json"
	"os"
	"time"
)

// implements json.Unmarshaler
type duration time.Duration

func (d *duration) UnmarshalJSON(data []byte) error {
	if len(data) < 3 {
		return nil
	}
	dd, err := time.ParseDuration(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}
	*d = duration(dd)
	return nil
}

// Config.NonGetMode
const (
	ModePass = iota
	ModeBlock
	ModeCache
)

type config struct {
	LocalAddr  string `json:"local_addr"`  // Local listening address
	TargetAddr string `json:"target_addr"` // Proxy target
	LogFile    string `json:"log_file"`    // Specify a log destination
	LogLevel   int    // Specify a log level: debug|info|warning|error
	NonGetMode int    // How to deal with non-GET requests: pass|block|cache
	CacheSize  int    `json:"cache_size"` // Max cache size in bytes, default 1 GB
	CliSocket  string `json:"cli_socket"` // Socket file for the cli tool

	// Bypass caching if Cookie or Authorization is presented in request headers? -
	// When set to false, both headers are stripped to prevent
	// user-specific or privileged content being cached.
	// Note that some auth mechanisms like CSRF token validation may still break
	// despite this option being turned on, due to the nature of reverse proxy
	AllowAuth bool `json:"allow_auth"`

	// Deduplicate if different URLs return same response?
	CacheUnique bool `json:"cache_unique"`

	// Detect mobile UA and cache the responses separately?
	CacheMobile bool `json:"cache_mobile"`

	// Password used in request header defined by RefreshHeader to force a cache update
	RefreshPw string `json:"refresh_password"`

	// Request header name to carry your refresh password
	RefreshHeader string `json:"refresh_header"`

	// Track access count within this time period for each cache entry
	LfuTime time.Duration

	// Fresh requests will go stale and fall into LFU list after this much of time
	ProtectionExpire time.Duration

	// Timeouts reserved for dealing with theoretical slow request DoS

	IdleTimeout  time.Duration // Corresponds to http.Server.IdleTimeout
	ReadTimeout  time.Duration // Corresponds to http.Server.ReadTimeout
	WriteTimeout time.Duration // Corresponds to http.Server.WriteTimeout
}

var Config config = config{
	LocalAddr:        ":3456",
	LogLevel:         LogWarn,
	CacheSize:        1 << 30,
	NonGetMode:       ModePass,
	LfuTime:          30 * time.Minute,
	ProtectionExpire: 30 * time.Minute,
}

var LastConfFile string

func LoadConfFile(file string, isCli bool) {
	LastConfFile = file
	data, err := os.ReadFile(file)
	if err != nil {
		Log(LogWarn, "can't read config file %s, default config values will be used. #%s", file, err)
		return
	}

	jsonData := struct {
		*config
		LogLevel         string   `json:"log_level"`
		NonGetMode       string   `json:"non_get_mode"`
		LfuTime          duration `json:"lfu_time"`
		ProtectionExpire duration `json:"protection_expire"`
		IdleTimeout      duration `json:"idle_timeout"`
		ReadTimeout      duration `json:"read_timeout"`
		WriteTimeout     duration `json:"write_timeout"`
	}{config: &Config}

	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		Log(LogWarn, "invalid config file %s, default config values will be used. #%s", file, err)
		return
	}

	switch jsonData.LogLevel {
	case "debug":
		Config.LogLevel = LogDebug
	case "info":
		Config.LogLevel = LogInfo
	case "warning":
		Config.LogLevel = LogWarn
	case "error":
		Config.LogLevel = LogErr
	}

	if Config.LogFile != "" && !isCli {
		setLogFile(Config.LogFile)
	}

	switch jsonData.NonGetMode {
	case "pass":
		Config.NonGetMode = ModePass
	case "block":
		Config.NonGetMode = ModeBlock
	case "cache":
		Config.NonGetMode = ModeCache
	}

	Config.LfuTime = time.Duration(jsonData.LfuTime)
	Config.IdleTimeout = time.Duration(jsonData.IdleTimeout)
	Config.ReadTimeout = time.Duration(jsonData.ReadTimeout)
	Config.WriteTimeout = time.Duration(jsonData.WriteTimeout)
	Config.ProtectionExpire = time.Duration(jsonData.ProtectionExpire)

	Log(LogInfo, "config file loaded, current conf values: %+v", Config)
}
