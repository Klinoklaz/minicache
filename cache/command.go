package cache

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
)

func humanReadableSize(bytes int) (float64, string) {
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

// prints comprehensive cache entry info for the cli tool
func Show(conn net.Conn, key string) (int, error) {
	cachePool.mtx.RLock()
	c := cachePool.pool[key]
	cachePool.mtx.RUnlock()
	if c == nil {
		return conn.Write([]byte{'\n'})
	}

	size, unit := humanReadableSize(len(c.Body))

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
		"Status code:\t%d\n"+
		"Status:\t\t%c\n"+
		"Access count:\t%d\n"+
		"Hash:\t\t%s\n"+
		"Protected at:\t%s\n"+
		"All URIs:\t%s\n"+
		"Unescaped URIs:\t%s\n",
		size, unit,
		len(c.Header),
		c.StatusCode,
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

// prints basic info of a list of cache entries for cli tool
func List(conn net.Conn, arg string) (int, error) {
	var n int
	n, err := conn.Write([]byte("Size\t\tStatus\tAccess\tURI\n"))
	if err != nil {
		return n, err
	}

	var orderBy string
	var limit int // TODO: this should be a paging param instead of limit
	argc, _ := fmt.Sscanf(arg, "%s %d", &orderBy, &limit)
	if argc < 2 || limit == 0 {
		limit = 20
	}
	if argc < 1 {
		orderBy = "a"
	}

	// prepare for the sorting
	var cmp func(a, b *Cache) int
	switch {
	case orderBy == "s" && limit < 0: // size asc
		cmp = func(a, b *Cache) int { return len(a.Body) - len(b.Body) }
	case orderBy == "s" && limit > 0: // size desc
		cmp = func(a, b *Cache) int { return len(b.Body) - len(a.Body) }
	case orderBy == "a" && limit < 0: // access count asc
		cmp = func(a, b *Cache) int { return a.accessCnt - b.accessCnt }
	case orderBy == "a" && limit > 0: // access count desc
		cmp = func(a, b *Cache) int { return b.accessCnt - a.accessCnt }
	case orderBy == "f" && limit < 0: // access frequency asc
		cmp = func(a, b *Cache) int {
			return a.accessCnt/int(time.Since(a.protectedAt).Seconds()) -
				b.accessCnt/int(time.Since(b.protectedAt).Seconds())
		}
	default: // access frequency desc
		cmp = func(a, b *Cache) int {
			return b.accessCnt/int(time.Since(b.protectedAt).Seconds()) -
				a.accessCnt/int(time.Since(a.protectedAt).Seconds())
		}
	}

	snapshot := make([]*Cache, 0, len(cachePool.pool))
	cachePool.mtx.RLock()
	for _, c := range cachePool.hashes {
		snapshot = append(snapshot, c)
	}
	cachePool.mtx.RUnlock()
	// sort the snapshot to mitigate lock contention
	slices.SortFunc(snapshot, cmp)

	if limit < 0 {
		limit = -limit
	}
	for i, c := range snapshot {
		if i > limit {
			break
		}
		size, unit := humanReadableSize(len(c.Body))
		m, err := fmt.Fprintf(conn, "%-16s%c\t%d\t%s\n",
			fmt.Sprintf("%.2f%s", size, unit), c.status, c.accessCnt, c.keys[0])
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
