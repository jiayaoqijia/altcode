package provider

import (
	"net"
	"net/http"
	"time"
)

// newStreamingClient returns an *http.Client suitable for long-running
// streaming SSE responses.
//
// The Go http.Client.Timeout field applies to the entire request lifetime
// including reading the response body — which for SSE is as long as the
// stream stays open. Setting it to a fixed duration (e.g. 5 minutes) caps
// how long an agent turn can run before the stream is killed mid-way, which
// surfaces as a bogus "timeout" error during long tool-using turns.
//
// Instead we leave Timeout unset and configure the Transport with
// granular timeouts: fast fails for connection and header problems, no
// cap on stream duration. User-initiated cancels still work via the
// request Context that the engine passes through.
func newStreamingClient() *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second, // wait up to 60s for HTTP headers
	}
	return &http.Client{Transport: tr}
}
