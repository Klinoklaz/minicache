package util

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

func GetRealIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// 1024 -> 1 KB, 1048576 -> 1 MB, etc.
func ByteSize(bytes int) (float64, string) {
	units := [4]string{"B", "KB", "MB", "GB"}
	size := float64(bytes)
	var i int
	for i < 3 {
		if size < 1024. {
			break
		}
		i++
		size /= 1024.
	}
	return size, units[i]
}

// 1k|1K|1KB -> 1024, etc., doesn't validate the format
func ParseByteSize(size string) int {
	digits, err := strconv.ParseFloat(
		regexp.MustCompile(`[0-9.]*`).FindStringSubmatch(size)[0], 32)
	if err != nil {
		return 0
	}
	if strings.Contains(size, "k") || strings.Contains(size, "K") {
		return int(digits * 1024)
	}
	if strings.Contains(size, "m") || strings.Contains(size, "M") {
		return int(digits * 1024 * 1024)
	}
	if strings.Contains(size, "g") || strings.Contains(size, "G") {
		return int(digits * 1024 * 1024 * 1024)
	}
	return int(digits)
}
