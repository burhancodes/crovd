package snapchat

import (
	"fmt"
	"regexp"

	"github.com/govdbot/govd/internal/extractors/raiden"
	"github.com/govdbot/govd/internal/models"
)

var snapchatHost = []string{"snapchat"}

var ShortExtractor = &models.Extractor{
	ID:          "snapchat",
	DisplayName: "Snapchat (Short)",

	URLPattern: regexp.MustCompile(`https?:\/\/t\.snapchat\.com\/(?P<id>[a-zA-Z0-9]+)`),
	Host:       snapchatHost,

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
	ID:          "snapchat",
	DisplayName: "Snapchat",

	URLPattern: regexp.MustCompile(`https?:\/\/(?:www\.)?(?:story\.)?snapchat\.com\/(?:spotlight|s|add|p|t)\/(?P<id>[a-zA-Z0-9_.-]+)`),
	Host:       snapchatHost,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		media, err := raiden.FetchMedia(ctx, raiden.RouteSnapchat, ctx.ContentURL)
		if err != nil {
			return nil, fmt.Errorf("snapchat extraction failed: %w", err)
		}
		return &models.ExtractorResponse{
			Media: media,
		}, nil
	},
}
