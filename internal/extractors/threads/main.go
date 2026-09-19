package threads

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/govdbot/govd/internal/extractors/raiden"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

var Extractor = &models.Extractor{
	ID:          "threads",
	DisplayName: "Threads",

	URLPattern: regexp.MustCompile(`https:\/\/(www\.)?threads\.[^\/]+\/(?:(?:@[^\/]+)\/)?p(?:ost)?\/(?P<id>[a-zA-Z0-9_-]+)`),
	Host:       []string{"threads"},

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		media, err := GetEmbedMedia(ctx)
		if err == nil {
			return &models.ExtractorResponse{Media: media}, nil
		}
		// fallback: raiden api
		raidenMedia, raidenErr := raiden.FetchMedia(ctx, raiden.RouteThreads, ctx.ContentURL)
		if raidenErr == nil {
			return &models.ExtractorResponse{Media: raidenMedia}, nil
		}
		return nil, fmt.Errorf("threads embed failed: %w; raiden fallback failed: %w", err, raidenErr)
	},
}

func GetEmbedMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	embedURL := fmt.Sprintf(
		"https://www.threads.net/@_/post/%s/embed",
		ctx.ContentID,
	)
	resp, err := ctx.Fetch(
		http.MethodGet,
		embedURL,
		&networking.RequestParams{
			Headers: headers,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	logger.WriteFile("threads_embed", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get embed media: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return ParseEmbedMedia(ctx, body)
}
