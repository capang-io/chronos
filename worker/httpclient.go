package worker

import (
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	httpClientInstance *http.Client
	httpClientOnce     sync.Once
)

// NewDefaultHTTPClient returns a *http.Client configured with a Transport that manages connection pooling.
func NewDefaultHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: tr,
		Timeout:   25 * time.Second,
	}
}

// SetHTTPClient allows injecting a custom *http.Client (useful for tests).
func SetHTTPClient(c *http.Client) {
	httpClientOnce.Do(func() {
		httpClientInstance = c
	})
}

// getHTTPClient returns a singleton http client, creating a default one if needed.
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		if httpClientInstance == nil {
			httpClientInstance = NewDefaultHTTPClient()
		}
	})
	return httpClientInstance
}
