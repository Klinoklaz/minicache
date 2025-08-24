package cache

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

func humanReadableSize(bytes int) (float64, string) {
	units := [4]string{"B", "KB", "MB", "GB"}
	size := float64(bytes)
	var i int
	for i = range units {
		base := 1 << (i * 10)
		size = size / float64(base)
		if size < 1024. {
			break
		}
	}
	return size, units[i]
}

// prints comprehensive cache entry info for the cli tool
func Show(conn net.Conn, key string) (int, error) {
	cachePool.mtx.RLock()
	c := cachePool.pool[key]
	cachePool.mtx.RUnlock()
	if c == nil {
		return conn.Write([]byte{'\n'})
	}

	size, unit := humanReadableSize(len(c.Content))

	var pTime string
	if !c.protectedAt.IsZero() {
		pTime = c.protectedAt.Format(time.StampNano) +
			" (" + time.Since(c.protectedAt).String() + ")"
	}
	unescaped := make([]string, len(c.keys))
	for j, s := range c.keys {
		unescaped[j], _ = url.PathUnescape(s)
	}

	return fmt.Fprintf(conn, "Content size:\t%.2f%s\n"+
		"Headers:\t%d\n"+
		"Status:\t\t%c\n"+
		"Access count:\t%d\n"+
		"Hash:\t\t%s\n"+
		"Protected at:\t%s\n"+
		"All URIs:\t%s\n"+
		"Unescaped URIs:\t%s\n",
		size, unit,
		len(c.Header),
		c.status,
		c.accessCnt,
		hex.EncodeToString(c.hash[:]),
		pTime,
		strings.Join(c.keys, "\n\t\t"),
		strings.Join(unescaped, "\n\t\t"))
}

// prints info about cache pool for cli tool
func Status(conn net.Conn) (int, error) {
	cachePool.mtx.RLock()
	size, unit := humanReadableSize(cachePool.size)
	n, err := fmt.Fprintf(conn, "Pool size:\t%.2f%s\n"+
		"Keys:\t\t%d\n"+
		"Hashes:\t\t%d\n"+
		"Protecting:\t%d\n"+
		"Evicting:\t%d\n",
		size, unit,
		len(cachePool.pool),
		len(cachePool.hashes),
		protectList.li.Len(),
		len(lfuList.li))
	cachePool.mtx.RUnlock()

	return n, err
}

// prints basic info of all cache entries for cli tool
func List(conn net.Conn) (int, error) {
	var n int
	n, err := conn.Write([]byte("Size\t\tStatus\tAccess\tURI\n"))
	if err != nil {
		return n, err
	}

	cachePool.mtx.RLock()
	defer cachePool.mtx.RUnlock()

	for k, c := range cachePool.pool {
		size, unit := humanReadableSize(len(c.Content))
		m, err := fmt.Fprintf(conn, "%-16s%c\t%d\t%s\n",
			fmt.Sprintf("%.2f%s", size, unit), c.status, c.accessCnt, k)
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// blocks most operations on cache
func Block() {
	cachePool.mtx.Lock()
}

func Unblock() {
	cachePool.mtx.Unlock()
}
