package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const (
	// agnesVideoPollInterval is the delay between status polls for Agnes video tasks.
	agnesVideoPollInterval = 1500 * time.Millisecond
	// agnesVideoRateLimitBackoff is the extra wait after an HTTP 429 from Agnes.
	agnesVideoRateLimitBackoff = 3 * time.Second
	// agnesVideoMaxPolls caps the poll loop (~10 min at 1.5s); the chain timeout bounds it too.
	agnesVideoMaxPolls = 400
)

// isAgnesVideoProvider reports whether the provider should route to the Agnes
// async videos API instead of the OpenAI-compatible chat completions path.
func isAgnesVideoProvider(providerName, apiBase string) bool {
	return strings.HasPrefix(providerName, "agnes") ||
		strings.Contains(strings.ToLower(apiBase), "agnes-ai.com")
}

// agnesVideoCreateURL returns the create endpoint, tolerating bases with or without a /v1 suffix.
func agnesVideoCreateURL(apiBase string) string {
	base := strings.TrimRight(apiBase, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/videos"
	}
	return base + "/v1/videos"
}

// agnesVideoPollURL returns the status endpoint on the Agnes host root — the
// poll API lives outside the /v1 prefix.
func agnesVideoPollURL(apiBase, videoID, model string) string {
	origin := strings.TrimSuffix(strings.TrimRight(apiBase, "/"), "/v1")
	return origin + "/agnesapi?video_id=" + url.QueryEscape(videoID) +
		"&model_name=" + url.QueryEscape(model)
}

// agnesAPIError builds an error from a non-2xx Agnes response, preferring the
// FastAPI-style {"detail": "..."} payload when present.
func agnesAPIError(scope string, status int, body []byte) error {
	var d struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &d); err == nil && d.Detail != "" {
		return fmt.Errorf("agnes: %s HTTP %d: %s", scope, status, d.Detail)
	}
	return fmt.Errorf("agnes: %s HTTP %d: %s", scope, status, truncateBytes(body, 500))
}

// callAgnesVideoGen generates a video via the Agnes async videos API.
// Flow: POST {base}/videos (mode text) → poll {origin}/agnesapi → download result URL.
// Image-to-video is unsupported in text mode — image data is ignored.
func (t *CreateVideoTool) callAgnesVideoGen(ctx context.Context, apiKey, apiBase, model, prompt string, duration int, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error) {
	if GetParamString(params, "image_base64", "") != "" {
		slog.Warn("create_video: image-to-video not supported by Agnes text mode, falling back to text-to-video")
	}

	body := map[string]any{
		"model":        model,
		"prompt":       prompt,
		"mode":         "text",
		"seconds":      strconv.Itoa(duration),
		"size":         GetParamString(params, "size", "720P"),
		"aspect_ratio": aspectRatio,
		"n":            1,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, agnesVideoCreateURL(apiBase), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{} // timeout governed by chain context
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, agnesAPIError("create", resp.StatusCode, respBody)
	}

	var createResp struct {
		VideoID string `json:"video_id"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &createResp); err != nil {
		return nil, nil, fmt.Errorf("parse create response: %w", err)
	}
	videoID := createResp.VideoID
	if videoID == "" {
		videoID = createResp.ID
	}
	if videoID == "" {
		return nil, nil, fmt.Errorf("no video_id in Agnes response: %s", truncateBytes(respBody, 300))
	}

	slog.Info("create_video: Agnes video task started, polling", "video_id", videoID, "model", model)

	videoURL, err := agnesPollVideo(ctx, client, apiKey, apiBase, videoID, model)
	if err != nil {
		return nil, nil, err
	}

	return agnesDownloadVideo(ctx, videoURL)
}

// agnesPollVideo polls the status endpoint until the task completes or fails.
func agnesPollVideo(ctx context.Context, client *http.Client, apiKey, apiBase, videoID, model string) (string, error) {
	pollURL := agnesVideoPollURL(apiBase, videoID, model)

	for i := range agnesVideoMaxPolls {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(agnesVideoPollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return "", fmt.Errorf("create poll request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("create_video: Agnes poll error, retrying", "error", err, "attempt", i+1)
			continue
		}
		pollBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusTooManyRequests:
			slog.Warn("create_video: Agnes rate limited, backing off", "attempt", i+1)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(agnesVideoRateLimitBackoff):
			}
			continue
		default:
			return "", agnesAPIError("poll", resp.StatusCode, pollBody)
		}

		var pollResp struct {
			Status string `json:"status"`
			URL    string `json:"url"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(pollBody, &pollResp); err != nil {
			return "", fmt.Errorf("parse poll response: %w", err)
		}

		switch pollResp.Status {
		case "completed":
			if pollResp.URL == "" {
				return "", fmt.Errorf("video completed but no url in response: %s", truncateBytes(pollBody, 300))
			}
			return pollResp.URL, nil
		case "failed":
			msg := "video generation failed"
			if pollResp.Error != nil && pollResp.Error.Message != "" {
				msg = pollResp.Error.Message
			}
			return "", fmt.Errorf("agnes: video generation failed: %s", msg)
		default:
			if (i+1)%20 == 0 {
				slog.Info("create_video: Agnes task pending", "attempt", i+1, "status", pollResp.Status)
			}
		}
	}

	return "", fmt.Errorf("agnes: video generation timed out after %d polls", agnesVideoMaxPolls)
}

// agnesDownloadVideo downloads the completed video and returns the raw bytes.
func agnesDownloadVideo(ctx context.Context, videoURL string) ([]byte, *providers.Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create download request: %w", err)
	}
	// The result URL may point at a third-party/presigned host — never attach the Agnes API key to it.

	client := &http.Client{} // timeout governed by chain context
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("download video: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("download error %d: %s", resp.StatusCode, truncateBytes(body, 300))
	}

	videoBytes, err := limitedReadAll(resp.Body, maxMediaDownloadBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read video data: %w", err)
	}

	return videoBytes, nil, nil
}
