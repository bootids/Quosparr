package meter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// NoRedirectClient returns a client that shares base's transport and jar but
// never follows a redirect: the 3xx response itself is returned. Quota
// providers must be built on such a client so a moving endpoint surfaces as
// an error instead of quietly fetching another host.
func NoRedirectClient(base *http.Client) *http.Client {
	c := *base
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// FetchError is the failure of one fetch, classified for the UI. Msg is
// already redacted and never contains a response body or credential.
type FetchError struct {
	Status Status
	// HTTP is the response status code when the failure is HTTP-level.
	HTTP int
	Msg  string
}

func (e *FetchError) Error() string {
	if e.HTTP != 0 {
		return fmt.Sprintf("%s: HTTP %d: %s", e.Status, e.HTTP, e.Msg)
	}
	return string(e.Status) + ": " + e.Msg
}

// HTTPStatus classifies a non-2xx status code.
func HTTPStatus(code int) Status {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return StatusAuth
	case http.StatusTooManyRequests:
		return StatusRateLimited
	}
	return StatusHTTP
}

// GetJSON sends GET url with the given headers plus the shared accept and
// user agent, and returns the body (at most 1 MiB) and the HTTP status.
// Transport failures return *FetchError (network or timeout); non-2xx
// statuses return *FetchError with the HTTPStatus class and a body-free
// message. A body larger than the limit is a parse failure.
func GetJSON(ctx context.Context, client Doer, url, userAgent string, headers map[string]string) ([]byte, int, error) {
	if client == nil {
		return nil, 0, &FetchError{Status: StatusNetwork, Msg: "no HTTP client"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, &FetchError{Status: StatusNetwork, Msg: "invalid request URL"}
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, transportError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.StatusCode, &FetchError{
			Status: HTTPStatus(resp.StatusCode),
			HTTP:   resp.StatusCode,
			Msg:    "quota endpoint rejected the request",
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, resp.StatusCode, transportError(ctx, err)
	}
	if len(body) > maxBodyBytes {
		return nil, resp.StatusCode, &FetchError{Status: StatusParse, Msg: "response body too large"}
	}
	return body, resp.StatusCode, nil
}

// transportError maps a transport failure to network or timeout.
func transportError(ctx context.Context, err error) *FetchError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &FetchError{Status: StatusTimeout, Msg: "quota request timed out"}
	}
	return &FetchError{Status: StatusNetwork, Msg: "quota request failed"}
}
