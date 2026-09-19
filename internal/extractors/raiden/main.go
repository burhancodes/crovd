package raiden

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

const (
	DefaultBaseURL = "https://raidenapi.duckdns.org"

	RouteInstagram = "/api/social/instagram"
	RouteTikTok    = "/api/social/tiktok"
	RouteX         = "/api/social/x"
	RouteThreads   = "/api/social/threads"
	RouteSnapchat  = "/api/social/snapchat"
	RouteDouyin    = "/api/social/douyin"
	RouteFacebook  = "/api/social/facebook"
	RoutePinterest = "/api/browse/pinterest"
	RouteReddit    = "/api/browse/reddit"
)

type Response struct {
	URL    string       `json:"url"`
	Title  string       `json:"title"`
	Detail string       `json:"detail"`
	Media  []*MediaItem `json:"media"`
}

type MediaItem struct {
	Label     string `json:"label"`
	Type      string `json:"type"`
	Download  string `json:"download"`
	Thumbnail string `json:"thumbnail"`
}

// FetchMedia performs a request to the Raiden / Rapigo API for the given route and target URL,
// mapping the returned media items to crovd's models.Media.
func FetchMedia(ctx *models.ExtractorContext, route string, targetURL string) (*models.Media, error) {
	apiURL := fmt.Sprintf("%s%s?url=%s", DefaultBaseURL, route, url.QueryEscape(targetURL))

	resp, err := ctx.Fetch(
		http.MethodGet,
		apiURL,
		&networking.RequestParams{
			Headers: map[string]string{
				"Accept": "application/json",
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to raiden api: %w", err)
	}
	defer resp.Body.Close()

	logName := fmt.Sprintf("raiden_%s_response", strings.ReplaceAll(strings.TrimPrefix(route, "/api/"), "/", "_"))
	logger.WriteFile(logName, resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read raiden response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp Response
		if jsonErr := sonic.ConfigFastest.Unmarshal(body, &errResp); jsonErr == nil && errResp.Detail != "" {
			return nil, fmt.Errorf("raiden api error (%s): %s", resp.Status, errResp.Detail)
		}
		return nil, fmt.Errorf("raiden api returned status: %s", resp.Status)
	}

	var response Response
	if err := sonic.ConfigFastest.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse raiden response: %w", err)
	}

	if len(response.Media) == 0 {
		if response.Detail != "" {
			return nil, fmt.Errorf("raiden api error: %s", response.Detail)
		}
		return nil, fmt.Errorf("no media found in raiden response")
	}

	media := ctx.NewMedia()
	if response.Title != "" {
		media.SetCaption(response.Title)
	}

	for i, item := range response.Media {
		if item.Download == "" {
			continue
		}

		mediaItem := media.NewItem()
		itemType := strings.ToLower(item.Type)
		downloadLower := strings.ToLower(item.Download)

		isVideo := itemType == "video" ||
			strings.Contains(downloadLower, ".mp4") ||
			strings.Contains(downloadLower, "mime_type=video")

		formatID := item.Label
		if formatID == "" {
			if isVideo {
				formatID = fmt.Sprintf("video_%d", i+1)
			} else {
				formatID = fmt.Sprintf("image_%d", i+1)
			}
		}

		if isVideo {
			format := &models.MediaFormat{
				FormatID:   formatID,
				Type:       database.MediaTypeVideo,
				URL:        []string{item.Download},
				VideoCodec: database.MediaCodecAvc,
				AudioCodec: database.MediaCodecAac,
			}
			if item.Thumbnail != "" {
				format.ThumbnailURL = []string{item.Thumbnail}
			}
			mediaItem.AddFormats(format)
		} else {
			mediaItem.AddFormats(&models.MediaFormat{
				FormatID: formatID,
				Type:     database.MediaTypePhoto,
				URL:      []string{item.Download},
			})
		}
	}

	if len(media.Items) == 0 {
		return nil, fmt.Errorf("no valid media items found in raiden response")
	}

	return media, nil
}
