package helper

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

func DoRequest(r *http.Request) (*http.Response, error) {
	fReq, err := http.NewRequest(r.Method, Config.TargetAddr+r.RequestURI, nil)
	if err != nil {
		return nil, fmt.Errorf("failed creating request for target: %w", err)
	}

	for h := range r.Header {
		fReq.Header.Add(h, r.Header.Get(h))
	}

	res, err := http.DefaultClient.Do(fReq)
	if err != nil {
		return nil, fmt.Errorf("failed sending request to target: %w", err)
	}
	return res, nil
}

// sends request directly to proxy target, bypass caching
func Forward(w http.ResponseWriter, r *http.Request) {
	res, err := DoRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		Log(LogErr, "target not reachable, %s %s #%s", r.Method, r.RequestURI, err)
		return
	}
	defer res.Body.Close()

	for h := range res.Header {
		w.Header().Add(h, res.Header.Get(h))
	}

	content, err := io.ReadAll(res.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		Log(LogErr, "could not read response from target, %s %s #%s", r.Method, r.RequestURI, err)
		return
	}

	w.WriteHeader(res.StatusCode)

	_, err = w.Write(content)
	if err != nil {
		Log(LogInfo, "client connection at %s is broken. #%s", r.RemoteAddr, err)
	}
}

var (
	proxyQueue chan bool

	queueing struct {
		cnt int // current number of requests in the queue
		mtx sync.RWMutex
	}
)

func dequeue() {
	interval := 1000 / Config.DequeueRate
	for {
		time.Sleep(time.Duration(interval) * time.Millisecond)
		proxyQueue <- true
	}
}

// forwards requests one by one in a constant speed.
// not particularly useful for rate limiting, but better than nothing
func Queue(w http.ResponseWriter, r *http.Request) {
	queueing.mtx.RLock()

	if Config.QueueCap > 0 && queueing.cnt > Config.QueueCap {
		queueing.mtx.RUnlock()
		return
	}

	// enqueue
	queueing.mtx.RUnlock()
	queueing.mtx.Lock()
	queueing.cnt++
	queueing.mtx.Unlock()
	Log(LogDebug, "queueing request %s %s", r.Method, r.RequestURI)

	// wait
	<-proxyQueue

	// dequeue
	queueing.mtx.Lock()
	queueing.cnt--
	queueing.mtx.Unlock()
	Log(LogDebug, "dequeue request %s %s", r.Method, r.RequestURI)

	Forward(w, r)
}
