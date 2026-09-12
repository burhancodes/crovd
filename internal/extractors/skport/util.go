package skport

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/bytedance/sonic"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
)

const (
	apiBase = "https://zonai.skport.com"
)

var webHeaders = map[string]string{
	"User-Agent":   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Accept":       "application/json, text/plain, */*",
	"Content-Type": "application/json",
	"Origin":       "https://www.skport.com",
	"Referer":      "https://www.skport.com/",
	"platform":     "3",
	"vname":        "1.0.0",
}

func makeSign(path, query, ts, token string) string {
	paramsJSON := `{"platform":"3","timestamp":"` + ts + `","dId":"","vName":"1.0.0"}`

	msg := path + query + ts + paramsJSON

	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(msg))
	hexStr := hex.EncodeToString(mac.Sum(nil))

	md5Hash := md5.Sum([]byte(hexStr))
	return hex.EncodeToString(md5Hash[:])
}

func buildHeaders(path, query, ts, token string) map[string]string {
	headers := make(map[string]string, len(webHeaders)+3)
	for k, v := range webHeaders {
		headers[k] = v
	}
	headers["sign"] = makeSign(path, query, ts, token)
	headers["timestamp"] = ts
	headers["cred"] = token
	return headers
}

func fetchToken(ctx *models.ExtractorContext) (string, error) {
	ts := fmt.Sprintf("%d", time.Now().Unix())

	resp, err := ctx.Fetch(
		http.MethodGet,
		apiBase+"/web/v1/auth/refresh",
		&networking.RequestParams{
			Headers: buildHeaders("/web/v1/auth/refresh", "", ts, ""),
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch token: %w", err)
	}
	defer resp.Body.Close()

	var authResp AuthResponse
	if err := sonic.ConfigFastest.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("failed to parse auth response: %w", err)
	}

	if authResp.Code != 0 || authResp.Data.Token == "" {
		return "", fmt.Errorf("failed to get token: code=%d", authResp.Code)
	}

	return authResp.Data.Token, nil
}

func GetItem(ctx *models.ExtractorContext, itemID string) (*Item, error) {
	token, err := fetchToken(ctx)
	if err != nil {
		return nil, err
	}

	query := "id=" + itemID
	ts := fmt.Sprintf("%d", time.Now().Unix())

	resp, err := ctx.Fetch(
		http.MethodGet,
		apiBase+"/web/v1/item?"+query,
		&networking.RequestParams{
			Headers: buildHeaders("/web/v1/item", query, ts, token),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch item: %w", err)
	}
	defer resp.Body.Close()

	var itemResp ItemResponse
	if err := sonic.ConfigFastest.NewDecoder(resp.Body).Decode(&itemResp); err != nil {
		return nil, fmt.Errorf("failed to parse item response: %w", err)
	}

	if itemResp.Code != 0 {
		return nil, fmt.Errorf("API error %d", itemResp.Code)
	}

	return &itemResp.Data.ItemInfo.Item, nil
}

func extractCaption(caption []*CaptionItem) string {
	var text string
	for _, c := range caption {
		if c.Kind == "text" && c.Text != nil {
			text += c.Text.Text
		}
	}
	return text
}

func collectImageURLs(item *Item) []string {
	seen := make(map[string]struct{})
	var urls []string

	add := func(u string) {
		if u != "" {
			if _, dup := seen[u]; !dup {
				seen[u] = struct{}{}
				urls = append(urls, u)
			}
		}
	}

	for _, entry := range item.Album {
		if entry.Image != nil {
			add(entry.Image.URL)
		}
	}

	if len(urls) == 0 && item.Document != nil {
		for _, blockID := range item.Document.BlockIDs {
			block, ok := item.Document.BlockMap[blockID]
			if !ok || block.Kind != "image" || block.Image == nil {
				continue
			}
			add(block.Image.URL)
		}
	}

	if len(urls) == 0 && item.Cover != nil && item.Cover.URL != "" {
		add(item.Cover.URL)
	}

	return urls
}
