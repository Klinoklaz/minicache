package util

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

var client *http.Client = &http.Client{Timeout: 15 * time.Second}

func DoRequest(r *http.Request) (*http.Response, error) {
	fReq, err := http.NewRequest(r.Method, Config.TargetAddr+r.RequestURI, r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed creating request for target: %w", err)
	}

	for h := range r.Header {
		fReq.Header.Add(h, r.Header.Get(h))
	}

	res, err := client.Do(fReq)
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
