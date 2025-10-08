package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	LocalAddr   string   `json:"local_addr"` // Local listening address
	Target      string   `json:"target"`     // Proxy target
	TargetURL   *url.URL // Parsing result of Target
	LogFile     string   `json:"log_file"` // Specify a log destination
	LogLevel    int      // Specify a log level: debug|info|warning|error
	NonGetMode  int      // How to deal with non-GET requests: pass|block|cache
	CacheSize   int      `json:"cache_size"`   // Max cache size in bytes, default 1 GB
	CliSocket   string   `json:"cli_socket"`   // Socket file for the cli tool
	CacheStatus []int    `json:"cache_status"` // Which response status codes are cacheable

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

	// Fresh requests will go stale and fall into LFU list after this amount of time
	ProtectionExpire time.Duration

	// Cancel proxy request if target doesn't respond within this amount of time
	TargetTimeout time.Duration

	// TargetRateLimit[0] specifies minimum interval
	// between each request sent to target in millisecond,
	// 0 (default) as well as any negative number means no limit;
	// TargetRateLimit[1] specifies maximum waiting connections,
	// 0 (default) as well as any negative number means no limit
	TargetRateLimit [2]int `json:"target_rate_limit"`

	// Timeouts reserved for dealing with theoretical slow request DoS,
	// these won't be affected by config reload

	IdleTimeout  time.Duration // Corresponds to http.Server.IdleTimeout
	ReadTimeout  time.Duration // Corresponds to http.Server.ReadTimeout
	WriteTimeout time.Duration // Corresponds to http.Server.WriteTimeout
}

// use an exported global for simplicity,
// most fields shouldn't be arbitrarily modified during runtime
var Config config = config{
	LocalAddr:        ":3456",
	LogLevel:         LogLevelWarn,
	CacheSize:        1 << 30,
	NonGetMode:       ModePass,
	LfuTime:          30 * time.Minute,
	ProtectionExpire: 30 * time.Minute,
	TargetTimeout:    15 * time.Second,
	CacheStatus:      []int{http.StatusOK},
}

// useful in config reloading
var LastConfFile string

func LoadConfFile(file string, isCli bool) error {
	LastConfFile = file
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
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
		TargetTimeout    duration `json:"target_timeout"`
	}{config: &Config}

	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		return fmt.Errorf("parse file %s: %w", file, err)
	}

	switch jsonData.LogLevel {
	case "debug":
		Config.LogLevel = LogLevelDebug
	case "info":
		Config.LogLevel = LogLevelInfo
	case "warning":
		Config.LogLevel = LogLevelWarn
	case "error":
		Config.LogLevel = LogLevelErr
	}

	if Config.LogFile != "" && !isCli {
		setLogFile(Config.LogFile)
	}
	target, err := url.Parse(Config.Target)
	// tolerate error on config reloading,
	// can't simply terminate program if target is invalid
	if err == nil && target.Scheme != "" && target.Host != "" {
		Config.TargetURL = target
	} else if err != nil {
		LogErr("invalid proxy target %s #%s", Config.Target, err)
	} else {
		LogErr("invalid or empty proxy target %s", Config.Target)
	}

	switch jsonData.NonGetMode {
	case "pass":
		Config.NonGetMode = ModePass
	case "block":
		Config.NonGetMode = ModeBlock
	case "cache":
		Config.NonGetMode = ModeCache
	}

	Config.IdleTimeout = time.Duration(jsonData.IdleTimeout)
	Config.ReadTimeout = time.Duration(jsonData.ReadTimeout)
	Config.WriteTimeout = time.Duration(jsonData.WriteTimeout)
	// avoid overriding default if not set
	if jsonData.LfuTime > 0 {
		Config.LfuTime = time.Duration(jsonData.LfuTime)
	}
	if jsonData.TargetTimeout > 0 {
		Config.TargetTimeout = time.Duration(jsonData.TargetTimeout)
	}
	if jsonData.ProtectionExpire > 0 {
		Config.ProtectionExpire = time.Duration(jsonData.ProtectionExpire)
	}

	LogInfo("config file loaded, current config values: %+v", Config)
	return nil
}
