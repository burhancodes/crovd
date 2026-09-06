package main

import (
	"fmt"
	"os"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/extractors"
	"github.com/govdbot/govd/internal/extractors/instagram"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"go.uber.org/zap/zapcore"
)

func main() {
	logger.Init()
	defer logger.L.Sync()

	if os.Getenv("BOT_TOKEN") == "" {
		os.Setenv("BOT_TOKEN", "123456:dummy-token-for-testing")
	}

	config.Load()
	logger.SetLevel(zapcore.DebugLevel)

	if len(os.Args) < 2 {
		fmt.Println("Usage: igtest <instagram_url>")
		return
	}

	url := os.Args[1]
	ctx := extractors.FromURL(url)
	if ctx == nil {
		fmt.Println("RESULT: no extractor matched")
		return
	}
	fmt.Printf("matched extractor: %s (content id: %s)\n", ctx.Extractor.ID, ctx.ContentID)

	media, err1 := instagram.GetGQLMedia(ctx)
	fmt.Printf("method 1 (GQL): media=%s err=%v\n", describe(media), err1)

	media, err2 := instagram.GetEmbedMedia(ctx)
	fmt.Printf("method 2 (embed): media=%s err=%v\n", describe(media), err2)

	media, err3 := instagram.GetIGramPost(ctx)
	fmt.Printf("method 3 (igram): media=%s err=%v\n", describe(media), err3)

	media, err4 := instagram.GetDDInstaMedia(ctx)
	fmt.Printf("method 4 (ddinstagram): media=%s err=%v\n", describe(media), err4)

	media, err5 := instagram.GetYtDlpMedia(ctx)
	fmt.Printf("method 5 (yt-dlp): media=%s err=%v\n", describe(media), err5)
}

func describe(m *models.Media) string {
	if m == nil {
		return "<nil>"
	}
	out := fmt.Sprintf("%d items [", len(m.Items))
	for _, item := range m.Items {
		out += fmt.Sprintf("%d formats ", len(item.Formats))
	}
	return out + "]"
}
