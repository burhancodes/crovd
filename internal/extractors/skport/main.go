package skport

import (
	"fmt"
	"regexp"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
)

var Extractor = &models.Extractor{
	ID:          "skport",
	DisplayName: "Skport",

	URLPattern: regexp.MustCompile(
		`https?://(?:www\.)?skport\.com/article\?id=(?P<id>\d+)`,
	),
	Host: []string{"skport"},

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		media, err := GetMedia(ctx)
		if err != nil {
			return nil, err
		}
		return &models.ExtractorResponse{
			URL:   ctx.ContentURL,
			Media: media,
		}, nil
	},
}

func GetMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	itemID := ctx.ContentID
	ctx.Warnf("itemID: %s, matchGroups: %v", itemID, ctx.MatchGroups)

	post, err := GetItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}

	media := ctx.NewMedia()
	media.SetCaption(extractCaption(post.Caption))

	urls := collectImageURLs(post)
	if len(urls) == 0 {
		return nil, util.ErrUnavailable
	}

	for _, imgURL := range urls {
		mediaItem := media.NewItem()
		mediaItem.AddFormats(&models.MediaFormat{
			Type:     database.MediaTypePhoto,
			FormatID: "image",
			URL:      []string{imgURL},
		})
	}

	return media, nil
}
