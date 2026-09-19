package douyin

import (
	"fmt"
	"regexp"

	"github.com/govdbot/govd/internal/extractors/raiden"
	"github.com/govdbot/govd/internal/models"
)

var douyinHost = []string{"douyin"}

var ShortExtractor = &models.Extractor{
	ID:          "douyin",
	DisplayName: "Douyin (Short)",

	URLPattern: regexp.MustCompile(`https?:\/\/v\.douyin\.com\/(?P<id>[a-zA-Z0-9_-]+)`),
	Host:       douyinHost,

	Redirect: true,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		redirectURL, err := ctx.FetchLocation(ctx.ContentURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get url location: %w", err)
		}
		return &models.ExtractorResponse{URL: redirectURL}, nil
	},
}

var Extractor = &models.Extractor{
	ID:          "douyin",
	DisplayName: "Douyin",

	URLPattern: regexp.MustCompile(`https?:\/\/(?:(?:www|v)\.)?douyin\.com\/(?:video|note)\/(?P<id>[0-9]+)`),
	Host:       douyinHost,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		media, err := raiden.FetchMedia(ctx, raiden.RouteDouyin, ctx.ContentURL)
		if err != nil {
			return nil, fmt.Errorf("douyin extraction failed: %w", err)
		}
		return &models.ExtractorResponse{
			Media: media,
		}, nil
	},
}
