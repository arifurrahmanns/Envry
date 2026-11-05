package checks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

const (
	// MaxLinks caps how many links one CheckLinks call requests.
	MaxLinks = 50
	// linkTimeout bounds each link, redirects included.
	linkTimeout = 5 * time.Second
	// linkWorkers is how many links are requested at once.
	linkWorkers = 6
	userAgent   = "Envry-LinkCheck/1.0 (+https://github.com/arifurrahmanns/envry)"
)

// LinkResult is the outcome of requesting one link.
type LinkResult struct {
	URL string `json:"url"`
	// Status is the final HTTP status after redirects; 0 when the request
	// failed, in which case Error says why.
	Status int    `json:"status"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// CheckLinks requests each http(s) URL with HEAD, retrying with GET when
// the server does not allow HEAD, and reports the final status. Only the
// first MaxLinks URLs are requested. A nil client uses a default one.
func CheckLinks(ctx context.Context, client *http.Client, urls []string) []LinkResult {
	if client == nil {
		client = &http.Client{}
	}
	if len(urls) > MaxLinks {
		urls = urls[:MaxLinks]
	}
	results := make([]LinkResult, len(urls))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(linkWorkers, len(urls)) {
		wg.Go(func() {
			for i := range jobs {
				results[i] = checkLink(ctx, client, urls[i])
			}
		})
	}
	for i := range urls {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

func checkLink(ctx context.Context, client *http.Client, raw string) LinkResult {
	res := LinkResult{URL: raw}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		res.Error = "not an http(s) URL"
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, linkTimeout)
	defer cancel()

	status, err := request(ctx, client, http.MethodHead, raw)
	if err == nil && (status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented) {
		status, err = request(ctx, client, http.MethodGet, raw)
	}
	if err != nil {
		res.Error = describe(err)
		return res
	}
	res.Status = status
	res.OK = status < 400
	return res
}

func request(ctx context.Context, client *http.Client, method, target string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	// Only the status matters; drain a little so the connection can be reused.
	_, _ = io.CopyN(io.Discard, resp.Body, 4<<10)
	resp.Body.Close()
	return resp.StatusCode, nil
}

// describe turns a request error into a short, readable reason.
func describe(err error) string {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err):
		return fmt.Sprintf("timed out after %s", linkTimeout)
	case errors.As(err, &dnsErr):
		return "host not found"
	case errors.As(err, &opErr) && opErr.Op == "dial":
		return "could not connect"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err.Error()
	}
	return err.Error()
}
