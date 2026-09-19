package instagram

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/extractors/raiden"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"
)

var instagramHost = []string{"instagram"}

var Extractor = &models.Extractor{
	ID:          "instagram",
	DisplayName: "Instagram",

	URLPattern: regexp.MustCompile(`https:\/\/(www\.)?(?:dd)?instagram\.com\/(reels?|p|tv)\/(?P<id>[a-zA-Z0-9_-]+)`),
	Host:       instagramHost,
	Redirect:   false,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		// method 1: get media from GQL web API
		media, err1 := GetGQLMedia(ctx)
		if err1 == nil {
			return &models.ExtractorResponse{
				Media: media,
			}, nil
		}
		// method 2: get media from embed page
		media, err2 := GetEmbedMedia(ctx)
		if err2 == nil {
			return &models.ExtractorResponse{
				Media: media,
			}, nil
		}
		// method 3: get media from 3rd party service (raidenapi - main fallback)
		media, err3 := GetRaidenMedia(ctx)
		if err3 == nil {
			return &models.ExtractorResponse{
				Media: media,
			}, nil
		}
		// method 4: get media via yt-dlp (native mobile api / cookies fallback)
		media, err4 := GetYtDlpMedia(ctx)
		if err4 == nil {
			return &models.ExtractorResponse{
				Media: media,
			}, nil
		}
		return nil, fmt.Errorf("all methods failed: %w; %w; %w; %w", err1, err2, err3, err4)
	},
}

var StoriesExtractor = &models.Extractor{
	ID:          "instagram",
	DisplayName: "Instagram Stories",

	URLPattern: regexp.MustCompile(`https:\/\/(www\.)?(?:dd)?instagram\.com\/stories\/[a-zA-Z0-9._]+\/(?P<id>\d+)`),
	Host:       instagramHost,
	Hidden:     true,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		// method 1: get story via raidenapi
		media, err1 := GetRaidenMedia(ctx)
		if err1 == nil {
			return &models.ExtractorResponse{
				Media: media,
			}, nil
		}
		// method 2: get story via Instagram native private API (i.instagram.com)
		media, err2 := GetNativeStory(ctx)
		if err2 == nil {
			return &models.ExtractorResponse{
				Media: media,
			}, nil
		}
		// method 3: get story via yt-dlp (uses instagram.txt cookies)
		media, err3 := GetYtDlpMedia(ctx)
		if err3 == nil {
			return &models.ExtractorResponse{
				Media: media,
			}, nil
		}
		return nil, fmt.Errorf("all methods failed: %w; %w; %w", err1, err2, err3)
	},
}

var ShareURLExtractor = &models.Extractor{
	ID:          "instagram",
	DisplayName: "Instagram (Share)",

	URLPattern: regexp.MustCompile(`https?:\/\/(www\.)?(?:dd)?instagram\.com\/share\/((reels?|video|s|p)\/)?(?P<id>[^\/\?]+)`),
	Host:       instagramHost,

	Redirect: true,

	GetFunc: func(ctx *models.ExtractorContext) (*models.ExtractorResponse, error) {
		redirectURL, err := ctx.FetchLocation(ctx.ContentURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get url location: %w", err)
		}
		return &models.ExtractorResponse{URL: redirectURL}, nil
	},
}

func GetGQLMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	graphData, err := GetGQLData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get graph data: %w", err)
	}
	return ParseGQLMedia(ctx, graphData.ShortcodeMedia)
}

func GetEmbedMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	embedURL := fmt.Sprintf(
		"https://www.instagram.com/p/%s/embed/captioned",
		ctx.ContentID,
	)
	resp, err := ctx.Fetch(
		http.MethodGet,
		embedURL,
		&networking.RequestParams{
			Headers: webHeaders,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	logger.WriteFile("ig_embed_response", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get embed page: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	graphData, err := ParseEmbedGQL(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse embed page: %w", err)
	}
	return ParseGQLMedia(ctx, graphData)
}

func GetRaidenMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	contentURL := fmt.Sprintf("https://www.instagram.com/p/%s/", ctx.ContentID)
	if strings.Contains(ctx.ContentURL, "/stories/") {
		contentURL = strings.Replace(ctx.ContentURL, "ddinstagram.com", "instagram.com", 1)
	}
	return raiden.FetchMedia(ctx, raiden.RouteInstagram, contentURL)
}

func GetYtDlpMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	ytURL := ctx.ContentURL
	ytDetails, ytErr := util.GetYtDlpMetadata(ctx.Context, ytURL)
	if ytErr != nil {
		return nil, fmt.Errorf("yt-dlp fallback failed: %w", ytErr)
	}

	media := ctx.NewMedia()
	media.SetCaption(ytDetails.Description)
	if media.Caption == "" {
		media.SetCaption(ytDetails.Title)
	}

	isPhotoPost := strings.Contains(ytURL, "/p/") || strings.Contains(ytURL, "/photo/")

	var imageURLs []string
	if isPhotoPost {
		seenURLs := make(map[string]bool)
		for _, t := range ytDetails.Thumbnails {
			if t.URL == "" {
				continue
			}
			cleanURL := strings.Split(t.URL, "?")[0]
			if !seenURLs[cleanURL] {
				seenURLs[cleanURL] = true
				imageURLs = append(imageURLs, t.URL)
			}
		}
	}

	if len(imageURLs) > 0 {
		for _, imgURL := range imageURLs {
			item := media.NewItem()
			item.AddFormats(&models.MediaFormat{
				Type:     database.MediaTypePhoto,
				FormatID: "image",
				URL:      []string{imgURL},
			})
		}
		return media, nil
	}

	var bestWidth, bestHeight int32
	var bestBitrate int64
	vcodec := database.MediaCodecAvc
	acodec := database.MediaCodecAac
	for _, f := range ytDetails.Formats {
		parsedVCodec := util.ParseVideoCodec(f.VCodec)
		if parsedVCodec == "" {
			continue
		}
		if int64(f.Bitrate) > bestBitrate || (f.Width > bestWidth && f.Height > bestHeight) {
			bestWidth = f.Width
			bestHeight = f.Height
			bestBitrate = int64(f.Bitrate)
			vcodec = parsedVCodec
			parsedACodec := util.ParseAudioCodec(f.ACodec)
			if parsedACodec != "" {
				acodec = parsedACodec
			}
		}
	}

	item := media.NewItem()
	item.AddFormats(&models.MediaFormat{
		Type:       database.MediaTypeVideo,
		FormatID:   "ytdlp_best",
		URL:        []string{ytURL},
		VideoCodec: vcodec,
		AudioCodec: acodec,
		Width:      bestWidth,
		Height:     bestHeight,
		Duration:   int32(ytDetails.Duration),
		Bitrate:    bestBitrate,
		DownloadSettings: &models.DownloadSettings{
			YtDlpMediaURL: ytURL,
		},
	})
	return media, nil
}

