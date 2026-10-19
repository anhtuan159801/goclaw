package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsAgnesVideoProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		apiBase  string
		want     bool
	}{
		{name: "name prefix", provider: "agnes", apiBase: "https://example.com/v1", want: true},
		{name: "name with suffix", provider: "agnes-backup", apiBase: "https://example.com/v1", want: true},
		{name: "base host match", provider: "custom", apiBase: "https://apihub.agnes-ai.com/v1", want: true},
		{name: "base host match case-insensitive", provider: "custom", apiBase: "https://APIHUB.AGNES-AI.COM/v1", want: true},
		{name: "name prefix with empty base", provider: "agnes", apiBase: "", want: true},
		{name: "openai base", provider: "openai", apiBase: "https://api.openai.com/v1", want: false},
		{name: "gemini provider", provider: "gemini", apiBase: "https://generativelanguage.googleapis.com/v1beta", want: false},
		{name: "unrelated host", provider: "custom", apiBase: "https://example.com/v1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAgnesVideoProvider(tt.provider, tt.apiBase); got != tt.want {
				t.Fatalf("isAgnesVideoProvider(%q, %q) = %v, want %v", tt.provider, tt.apiBase, got, tt.want)
			}
		})
	}
}

func TestCallAgnesVideoGen_HappyPath(t *testing.T) {
	wantVideo := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x6d, 0x70, 0x34, 0x32}

	var createBody map[string]any
	var createAuth, pollAuth, downloadAuth string
	var pollVideoID, pollModel string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos":
			createAuth = r.Header.Get("Authorization")
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &createBody); err != nil {
				t.Errorf("parse create body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"video_id": "video_abc",
				"status":   "queued",
				"progress": 0,
			})
		case "/agnesapi":
			pollAuth = r.Header.Get("Authorization")
			pollVideoID = r.URL.Query().Get("video_id")
			pollModel = r.URL.Query().Get("model_name")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":   "completed",
				"progress": 100,
				"url":      "http://" + r.Host + "/files/v.mp4",
			})
		case "/files/v.mp4":
			downloadAuth = r.Header.Get("Authorization")
			_, _ = w.Write(wantVideo)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tool := &CreateVideoTool{}
	params := map[string]any{"prompt": "a cat playing piano"}
	out, usage, err := tool.callAgnesVideoGen(context.Background(), "test-key", srv.URL+"/v1",
		"agnes-video-2.5-flash", "a cat playing piano", 8, "16:9", params)
	if err != nil {
		t.Fatalf("callAgnesVideoGen: %v", err)
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil", usage)
	}
	if string(out) != string(wantVideo) {
		t.Fatalf("video bytes mismatch: got %d bytes, want %d", len(out), len(wantVideo))
	}

	if createAuth != "Bearer test-key" {
		t.Errorf("create Authorization = %q, want Bearer test-key", createAuth)
	}
	if pollAuth != "Bearer test-key" {
		t.Errorf("poll Authorization = %q, want Bearer test-key", pollAuth)
	}
	if downloadAuth != "" {
		t.Errorf("download Authorization = %q, want empty (never leak key to result host)", downloadAuth)
	}
	if pollVideoID != "video_abc" {
		t.Errorf("poll video_id = %q, want video_abc", pollVideoID)
	}
	if pollModel != "agnes-video-2.5-flash" {
		t.Errorf("poll model_name = %q, want agnes-video-2.5-flash", pollModel)
	}

	wantFields := map[string]any{
		"model":        "agnes-video-2.5-flash",
		"prompt":       "a cat playing piano",
		"mode":         "text",
		"seconds":      "8",
		"size":         "720P",
		"aspect_ratio": "16:9",
	}
	for k, want := range wantFields {
		if got := createBody[k]; got != want {
			t.Errorf("create body %q = %#v, want %#v", k, got, want)
		}
	}
}

func TestCallAgnesVideoGen_FailedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos":
			_ = json.NewEncoder(w).Encode(map[string]any{"video_id": "video_bad"})
		case "/agnesapi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "failed",
				"error":  map[string]any{"message": "Invalid reference media"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tool := &CreateVideoTool{}
	_, _, err := tool.callAgnesVideoGen(context.Background(), "k", srv.URL+"/v1",
		"agnes-video-2.5-flash", "p", 4, "16:9", nil)
	if err == nil {
		t.Fatal("expected error for failed status")
	}
	if !strings.Contains(err.Error(), "Invalid reference media") {
		t.Fatalf("error %q does not contain failure message", err)
	}
}

func TestCallAgnesVideoGen_Create400Detail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"detail": "size must be 720P"})
	}))
	defer srv.Close()

	tool := &CreateVideoTool{}
	_, _, err := tool.callAgnesVideoGen(context.Background(), "k", srv.URL+"/v1",
		"agnes-video-2.5-flash", "p", 4, "16:9", map[string]any{"size": "1080P"})
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	if !strings.Contains(err.Error(), "size must be 720P") {
		t.Fatalf("error %q does not contain detail", err)
	}
}
