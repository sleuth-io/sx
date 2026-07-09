package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// completionTimeout bounds one hosted-API or Ollama completion. Large
// merges can legitimately take minutes on local models.
const completionTimeout = 5 * time.Minute

// maxResponseBytes bounds how much of a provider response we read; a
// completion payload is text, not a download.
const maxResponseBytes = 16 << 20

func decodeJSONBody(resp *http.Response, v any) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// postJSON sends a JSON request and decodes the JSON response into out.
// Non-2xx responses become errors carrying the (truncated) body, which
// is where every provider puts its human-readable failure reason.
func postJSON(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, completionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return decodeJSONBody(resp, out)
}

// extractJSON pulls a JSON document out of a model reply. Providers
// with prompt-based structured output are asked for bare JSON, but
// models still sometimes wrap it in markdown fences or a sentence of
// preamble; this recovers the document instead of failing the call.
func extractJSON(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}
	start := strings.IndexAny(trimmed, "{[")
	if start < 0 {
		return "", errors.New("model reply contains no JSON")
	}
	closer := "}"
	if trimmed[start] == '[' {
		closer = "]"
	}
	end := strings.LastIndex(trimmed, closer)
	if end <= start {
		return "", errors.New("model reply contains no complete JSON document")
	}
	candidate := trimmed[start : end+1]
	if !json.Valid([]byte(candidate)) {
		return "", errors.New("model reply is not valid JSON")
	}
	return candidate, nil
}

// finishStructured applies schema post-processing to a raw reply:
// extract the JSON document when the request asked for one.
func finishStructured(req Request, text string) (string, error) {
	if len(req.Schema) == 0 {
		return text, nil
	}
	return extractJSON(text)
}
