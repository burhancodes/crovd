package util

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/govdbot/govd/internal/config"
)

type YtDlpResponse struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Uploader    string           `json:"uploader"`
	Duration    float64          `json:"duration"`
	Description string           `json:"description"`
	Formats     []YtDlpFormat    `json:"formats"`
	Thumbnails  []YtDlpThumbnail `json:"thumbnails"`
	Cookies     string           `json:"cookies"`
	IsLive      bool             `json:"is_live"`
	LiveStatus  string           `json:"live_status"`
}

type YtDlpThumbnail struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Preference int    `json:"preference"`
}

type YtDlpFormat struct {
	FormatID       string            `json:"format_id"`
	URL            string            `json:"url"`
	Ext            string            `json:"ext"`
	VCodec         string            `json:"vcodec"`
	ACodec         string            `json:"acodec"`
	Width          int32             `json:"width"`
	Height         int32             `json:"height"`
	Filesize       int64             `json:"filesize"`
	FilesizeApprox int64             `json:"filesize_approx"`
	Bitrate        float64           `json:"tbr"`
	Protocol       string            `json:"protocol"`
	Resolution     string            `json:"resolution"`
	HTTPHeaders    map[string]string `json:"http_headers"`
}

func resolveProxyArgs() []string {
	if config.Env != nil && config.Env.Proxy != "" {
		return []string{"--proxy", config.Env.Proxy}
	}
	return nil
}

func resolveCookieArgs(urlStr string) []string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil
	}
	cookieFile := ""
	host := strings.ToLower(parsedURL.Host)
	if strings.Contains(host, "youtube") || strings.Contains(host, "youtu.be") {
		cookieFile = "youtube.txt"
	} else if strings.Contains(host, "tiktok.com") {
		cookieFile = "tiktok.txt"
	} else if strings.Contains(host, "twitter.com") || strings.Contains(host, "x.com") {
		cookieFile = "twitter.txt"
	} else if strings.Contains(host, "instagram.com") {
		cookieFile = "instagram.txt"
	}

	if cookieFile != "" {
		cookiePath := filepath.Join("private/cookies", cookieFile)
		if _, err := os.Stat(cookiePath); err == nil {
			return []string{"--cookies", cookiePath}
		}
	}
	return nil
}

func GetYtDlpMetadata(ctx context.Context, urlStr string) (*YtDlpResponse, error) {
	args := []string{
		"-j",
		"--skip-download",
		"--no-playlist",
		"--ignore-config",
		"--no-warnings",
		"--no-check-certificates",
	}
	args = append(args, resolveCookieArgs(urlStr)...)
	args = append(args, resolveProxyArgs()...)
	args = append(args, urlStr)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("yt-dlp failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run yt-dlp: %w", err)
	}

	var resp YtDlpResponse
	if err := sonic.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse yt-dlp output: %w", err)
	}

	return &resp, nil
}

// DownloadWithYtDlp downloads a video using yt-dlp directly.
// This is needed for sites like Instagram where CDN URLs require
// yt-dlp's session/cookie handling for proper downloads.
func DownloadWithYtDlp(ctx context.Context, urlStr string, outputPath string) error {
	args := []string{
		"-o", outputPath,
		"--no-playlist",
		"--ignore-config",
		"--no-warnings",
		"--no-check-certificates",
		"--merge-output-format", "mp4",
		"--no-part",
	}
	args = append(args, resolveCookieArgs(urlStr)...)
	args = append(args, resolveProxyArgs()...)
	args = append(args, urlStr)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("yt-dlp download failed: %s", string(output))
	}

	// yt-dlp may save to a slightly different path
	// (e.g. appending .mp4 if outputPath doesn't end with it)
	if _, err := os.Stat(outputPath); err == nil {
		return nil
	}

	// check for common yt-dlp naming patterns
	candidates := []string{
		outputPath + ".mp4",
		strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".mp4",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return os.Rename(candidate, outputPath)
		}
	}

	return fmt.Errorf("yt-dlp completed but output file not found at %s", outputPath)
}
