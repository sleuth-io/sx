package vault

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/sleuth-io/sx/internal/logger"
)

// transientStatusCodes are HTTP status codes that the Sleuth gateway
// occasionally returns when an upstream is briefly unavailable. Retrying
// is safe for the idempotent GETs used during `sx install`; the original
// failure mode (#124) was a 502 from skills.new that resolved on the
// next request.
var transientStatusCodes = map[int]bool{
	http.StatusBadGateway:         true, // 502
	http.StatusServiceUnavailable: true, // 503
	http.StatusGatewayTimeout:     true, // 504
}

// Retry tuning. Declared as vars rather than consts so tests can
// drop the backoff and avoid sleeping for seconds while exercising
// the multi-attempt paths.
var (
	httpRetryMaxAttempts    = 4
	httpRetryInitialBackoff = 500 * time.Millisecond
	httpRetryMaxBackoff     = 4 * time.Second
)

// doHTTPWithRetry executes req against client, retrying transient network
// errors and the 5xx codes listed in transientStatusCodes with exponential
// backoff. The intermediate responses have their body drained and closed
// so the underlying connection can be pooled for the retry; the final
// response (whether success or a transient code after the budget is
// exhausted) is returned with its body intact so the caller can read it.
//
// Only safe for idempotent requests — the body of req is not rewound
// between attempts.
func doHTTPWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	delay := httpRetryInitialBackoff

	for attempt := 1; ; attempt++ {
		resp, err := client.Do(req)
		if err == nil && !transientStatusCodes[resp.StatusCode] {
			return resp, nil
		}
		if attempt >= httpRetryMaxAttempts {
			// Budget exhausted: surface whatever we got. The caller
			// reads the response body to build its error message, so
			// leave it open on the transient-status branch.
			return resp, err
		}

		if err == nil {
			logger.Get().Debug(
				"retrying after transient HTTP status",
				"url", req.URL.String(),
				"status", resp.StatusCode,
				"attempt", attempt,
			)
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		} else {
			logger.Get().Debug(
				"retrying after network error",
				"url", req.URL.String(),
				"error", err,
				"attempt", attempt,
			)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
		if delay > httpRetryMaxBackoff {
			delay = httpRetryMaxBackoff
		}
	}
}
