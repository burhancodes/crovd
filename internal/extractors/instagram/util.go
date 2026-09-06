package instagram

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"

	"github.com/bytedance/sonic"
	"github.com/titanous/json5"
)

const (
	graphQLEndpoint = "https://www.instagram.com/graphql/query/"
	polarisAction   = "PolarisPostActionLoadPostQueryQuery"

	// GQL fingerprint constants — update these when Instagram returns 401
	//go:generate go run ../../../cmd/update-ig-constants
	gqlDocID        = "8845758582119845"
	gqlRolloutHash  = "1046919575"
	gqlBloksVersion = "394436feebb82fbc8bf09459d29e98a4182d7d9f4f36777d8278b409536b0803"
	gqlAsbdID       = "359341"
	gqlHiddenState  = "20702.HYP:instagram_web_pkg.2.1...0"

	igramHostname = "api-wh.igram.world"
	igramAPIBase  = "api.igram.world"
	igramHMACKey  = "75f2d70d3724f98e4a7d1ffd0ba9cfd907f3ae2632ee159980e2c521bff62358"
	igramStaticTS = 1771418815381 // parseInt("mls10xp1", 36)
)

var (
	embedPattern = regexp.MustCompile(
		`new ServerJS\(\)\);s\.handle\(({.*})\);requireLazy`)

	webHeaders = map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"Accept-Language":           "en-US,en;q=0.9",
		"Cache-Control":             "max-age=0",
		"Dnt":                       "1",
		"Priority":                  "u=0, i",
		"Sec-Ch-Ua":                 `"Chromium";v="133", "Google Chrome";v="133", "Not-A.Brand";v="99"`,
		"Sec-Ch-Ua-Mobile":          "?0",
		"Sec-Ch-Ua-Platform":        "\"Windows\"",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
		"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
	}

	igramHeaders = map[string]string{
		"Referer": "https://igram.world/",
	}
)

func ParseGQLMedia(ctx *models.ExtractorContext, data *Media) (*models.Media, error) {
	var caption string
	if data.EdgeMediaToCaption != nil && len(data.EdgeMediaToCaption.Edges) > 0 {
		caption = data.EdgeMediaToCaption.Edges[0].Node.Text
	}

	media := ctx.NewMedia()
	media.SetCaption(caption)

	switch data.Typename {
	case "GraphVideo", "XDTGraphVideo":
		if data.VideoURL == "" {
			return nil, fmt.Errorf("video_url is empty (login may be required)")
		}
		var width, height int32
		if data.Dimensions != nil {
			width = data.Dimensions.Width
			height = data.Dimensions.Height
		}
		item := media.NewItem()
		item.AddFormats(&models.MediaFormat{
			FormatID:     "video",
			Type:         database.MediaTypeVideo,
			VideoCodec:   database.MediaCodecAvc,
			AudioCodec:   database.MediaCodecAac,
			URL:          []string{data.VideoURL},
			ThumbnailURL: []string{data.DisplayURL},
			Width:        width,
			Height:       height,
		})
	case "GraphImage", "XDTGraphImage":
		if data.DisplayURL == "" {
			return nil, fmt.Errorf("display_url is empty (login may be required)")
		}
		item := media.NewItem()
		item.AddFormats(&models.MediaFormat{
			FormatID: "image",
			Type:     database.MediaTypePhoto,
			URL:      []string{data.DisplayURL},
		})
	case "GraphSidecar", "XDTGraphSidecar":
		if data.EdgeSidecarToChildren != nil && len(data.EdgeSidecarToChildren.Edges) > 0 {
			edges := data.EdgeSidecarToChildren.Edges

			for i := range edges {
				node := edges[i].Node
				if err := parseMediaNode(media, node); err != nil {
					return nil, err
				}
			}
		}
	default:
		// Instagram may return unknown typenames; fall back to is_video / display_url
		logger.L.Warnf("unknown top-level typename %q, falling back to is_video", data.Typename)
		if err := parseMediaNode(media, data); err != nil {
			return nil, err
		}
	}

	if len(media.Items) == 0 {
		return nil, fmt.Errorf("no media found")
	}

	return media, nil
}

// parseMediaNode handles a single media node (top-level or sidecar child).
// It first tries the __typename field; if that is unrecognized it falls back
// to the is_video boolean and available URL fields.
func parseMediaNode(media *models.Media, node *Media) error {
	switch node.Typename {
	case "GraphVideo", "XDTGraphVideo":
		if node.VideoURL == "" {
			return fmt.Errorf("video_url is empty for node (login may be required)")
		}
		var width, height int32
		if node.Dimensions != nil {
			width = node.Dimensions.Width
			height = node.Dimensions.Height
		}
		item := media.NewItem()
		item.AddFormats(&models.MediaFormat{
			FormatID:     "video",
			Type:         database.MediaTypeVideo,
			VideoCodec:   database.MediaCodecAvc,
			AudioCodec:   database.MediaCodecAac,
			URL:          []string{node.VideoURL},
			ThumbnailURL: []string{node.DisplayURL},
			Width:        width,
			Height:       height,
		})

	case "GraphImage", "XDTGraphImage":
		if node.DisplayURL == "" {
			return fmt.Errorf("display_url is empty for node (login may be required)")
		}
		item := media.NewItem()
		item.AddFormats(&models.MediaFormat{
			FormatID: "image",
			Type:     database.MediaTypePhoto,
			URL:      []string{node.DisplayURL},
		})

	default:
		// Unknown or omitted typename (embed sidecar children omit __typename entirely):
		// use is_video + available URLs as fallback
		if node.Typename != "" {
			logger.L.Warnf("unknown sidecar node typename %q, using is_video fallback", node.Typename)
		}
		switch {
		case node.IsVideo && node.VideoURL != "":
			var width, height int32
			if node.Dimensions != nil {
				width = node.Dimensions.Width
				height = node.Dimensions.Height
			}
			item := media.NewItem()
			item.AddFormats(&models.MediaFormat{
				FormatID:     "video",
				Type:         database.MediaTypeVideo,
				VideoCodec:   database.MediaCodecAvc,
				AudioCodec:   database.MediaCodecAac,
				URL:          []string{node.VideoURL},
				ThumbnailURL: []string{node.DisplayURL},
				Width:        width,
				Height:       height,
			})
		case node.DisplayURL != "":
			item := media.NewItem()
			item.AddFormats(&models.MediaFormat{
				FormatID: "image",
				Type:     database.MediaTypePhoto,
				URL:      []string{node.DisplayURL},
			})
		default:
			logger.L.Warnf("skipping node with typename %q: no usable URL found", node.Typename)
		}
	}
	return nil
}

func ParseEmbedGQL(body []byte) (*Media, error) {
	match := embedPattern.FindSubmatch(body)
	if len(match) < 2 {
		return nil, fmt.Errorf("gql json not found")
	}
	jsonData := match[1]

	var data map[string]any
	if err := json5.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	igCtx := util.TraverseJSON(data, "contextJSON")
	if igCtx == nil {
		return nil, fmt.Errorf("contextJSON not found")
	}
	var ctxJSON ContextJSON
	switch v := igCtx.(type) {
	case string:
		if err := json5.Unmarshal([]byte(v), &ctxJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal contextJSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("unexpected type for contextJSON: %T", v)
	}
	if ctxJSON.GqlData == nil {
		return nil, fmt.Errorf("gql_data not found")
	}
	if ctxJSON.GqlData.ShortcodeMedia == nil {
		return nil, fmt.Errorf("shortcode_media not found")
	}
	return ctxJSON.GqlData.ShortcodeMedia, nil
}

func IGramBodyFromURL(contentURL string) (io.Reader, error) {
	return igramBuildPayload(map[string]string{
		"target_url": contentURL,
	})
}

func IGramBodyFromParams(params map[string]string) (io.Reader, error) {
	return igramBuildPayload(params)
}

func igramBuildPayload(urlParams map[string]string) (io.Reader, error) {
	nowMs := time.Now().UnixMilli()
	serverMs := getIGramServerTime()

	drift := serverMs - nowMs
	var correction int64
	if drift >= 60000 || drift <= -60000 {
		correction = drift
	}
	ts := nowMs + correction

	// partial payload fields that get signed
	partial := map[string]any{
		"_sc": 0,
		"_ef": 0,
		"_df": 0,
	}
	for k, v := range urlParams {
		partial[k] = v
	}

	sig, err := igramSign(partial, ts)
	if err != nil {
		return nil, err
	}

	// assemble final payload
	final := make(map[string]any, len(partial)+5)
	for k, v := range partial {
		final[k] = v
	}
	final["ts"] = ts
	final["_ts"] = igramStaticTS
	final["_tsc"] = correction
	final["_sv"] = 2
	final["_s"] = sig

	jsonBytes, err := sonic.ConfigFastest.Marshal(final)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	return strings.NewReader(string(jsonBytes)), nil
}

func igramSign(partial map[string]any, ts int64) (string, error) {
	// sonic.ConfigStd sorts map keys alphabetically, matching
	// the signing: JSON.stringify(sorted_partial) + String(ts)
	jsonBytes, err := sonic.ConfigStd.Marshal(partial)
	if err != nil {
		return "", fmt.Errorf("failed to marshal partial payload: %w", err)
	}

	data := string(jsonBytes) + strconv.FormatInt(ts, 10)

	keyBytes, err := hex.DecodeString(igramHMACKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode HMAC key: %w", err)
	}

	mac := hmac.New(sha256.New, keyBytes)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func getIGramServerTime() int64 {
	apiURL := fmt.Sprintf("https://%s/msec", igramAPIBase)
	resp, err := http.Get(apiURL)
	if err != nil {
		return time.Now().UnixMilli()
	}
	defer resp.Body.Close()

	var result struct {
		Msec float64 `json:"msec"`
	}
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return time.Now().UnixMilli()
	}
	return int64(result.Msec * 1000)
}

func ParseIGramResponse(body []byte) (*IGramResponse, error) {
	// try to unmarshal as a single IGramMedia and then as a slice
	var media IGramMedia

	if err := sonic.ConfigFastest.Unmarshal(body, &media); err != nil {
		// try with slice
		var mediaList []*IGramMedia
		if err := sonic.ConfigFastest.Unmarshal(body, &mediaList); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		return &IGramResponse{
			Items: mediaList,
		}, nil
	}
	if media.Success != nil && !(*media.Success) {
		return nil, util.ErrUnavailable
	}
	return &IGramResponse{
		Items: []*IGramMedia{&media},
	}, nil
}

func GetCDNURL(contentURL string) (string, error) {
	if contentURL == "" {
		return "", fmt.Errorf("empty content URL from igram response")
	}
	parsedURL, err := url.Parse(contentURL)
	if err != nil {
		return "", fmt.Errorf("can't parse igram URL: %w", err)
	}
	queryParams, err := url.ParseQuery(parsedURL.RawQuery)
	if err != nil {
		return "", fmt.Errorf("can't unescape igram URL: %w", err)
	}
	// IGram may return the CDN URL directly or wrapped in a "uri" query param
	if cdnURL := queryParams.Get("uri"); cdnURL != "" {
		return cdnURL, nil
	}
	// fallback: use the URL as-is
	return contentURL, nil
}

func GetGQLData(ctx *models.ExtractorContext) (*GraphQLData, error) {
	graphHeaders, body, err := BuildGQLData()
	if err != nil {
		return nil, fmt.Errorf("failed to build GQL data: %w", err)
	}

	// Inject sessionid and csrftoken from private/cookies/instagram.txt into the GQL request
	if sessionID := GetCookieValue(ctx, "sessionid"); sessionID != "" {
		existingCookie := graphHeaders["cookie"]
		graphHeaders["cookie"] = existingCookie + "; sessionid=" + sessionID

		if dsUserID := GetCookieValue(ctx, "ds_user_id"); dsUserID != "" {
			graphHeaders["cookie"] += "; ds_user_id=" + dsUserID
			// Update payload user as well
			body["__user"] = dsUserID
		}

		if csrf := GetCookieValue(ctx, "csrftoken"); csrf != "" {
			graphHeaders["cookie"] += "; csrftoken=" + csrf
			graphHeaders["X-CSRFToken"] = csrf
		}
	}

	formData := url.Values{}
	for key, value := range body {
		formData.Set(key, value)
	}
	formData.Set("fb_api_caller_class", "RelayModern")
	formData.Set("fb_api_req_friendly_name", polarisAction)
	variables := map[string]any{
		"shortcode":               ctx.ContentID,
		"fetch_tagged_user_count": nil,
		"hoisted_comment_id":      nil,
		"hoisted_reply_id":        nil,
	}
	variablesJSON, err := sonic.ConfigFastest.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal variables: %w", err)
	}
	formData.Set("variables", string(variablesJSON))
	formData.Set("server_timestamps", "true")
	formData.Set("doc_id", gqlDocID) // Instagram GQL persisted query ID – update in util.go consts when 401 starts

	for key, value := range webHeaders {
		graphHeaders[key] = value
	}
	graphHeaders["Accept"] = "*/*"
	graphHeaders["Sec-Fetch-Dest"] = "empty"
	graphHeaders["Sec-Fetch-Mode"] = "cors"
	graphHeaders["Sec-Fetch-Site"] = "same-origin"
	graphHeaders["X-Requested-With"] = "XMLHttpRequest"
	graphHeaders["Origin"] = "https://www.instagram.com"
	graphHeaders["Referer"] = fmt.Sprintf("https://www.instagram.com/p/%s/", ctx.ContentID)
	resp, err := ctx.Fetch(
		http.MethodPost,
		graphQLEndpoint,
		&networking.RequestParams{
			Headers: graphHeaders,
			Body:    strings.NewReader(formData.Encode()),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	logger.WriteFile("iggql_api_response", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid response code: %s", resp.Status)
	}
	var response GraphQLResponse
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if response.Data == nil {
		return nil, fmt.Errorf("data is nil")
	}
	if response.Status != "ok" {
		return nil, fmt.Errorf("status is not ok: %s", response.Status)
	}
	if response.Data.ShortcodeMedia == nil {
		return nil, fmt.Errorf("shortcode_media is nil")
	}
	return response.Data, nil
}

func BuildGQLData() (map[string]string, map[string]string, error) {
	const (
		domain                = "www"
		requestID             = "b"
		clientCapabilityGrade = "EXCELLENT"
		sessionInternalID     = "7682474820282885466"
		apiVersion            = "1"
		appID                 = "936619743392459"
		loggedIn              = "0"
		cometRequestID        = "7"
		appVersion            = "0"
		pixelRatio            = "2"
		buildType             = "trunk"
	)
	session := "::" + util.RandomAlphaString(6)
	sessionData := util.RandomBase64(8)
	csrfToken := util.RandomBase64(32)
	deviceID := util.RandomBase64(24)
	machineID := util.RandomBase64(24)
	dynamicFlags := util.RandomBase64(154)
	clientSessionRnd := util.RandomBase64(154)
	jazoestBig, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate jazoest: %w", err)
	}
	jazoest := strconv.FormatInt(jazoestBig.Int64()+1, 10)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	cookies := []string{
		"csrftoken=" + csrfToken,
		"ig_did=" + deviceID,
		"wd=1280x720",
		"dpr=2",
		"mid=" + machineID,
		"ig_nrcb=1",
	}
	headers := map[string]string{
		"x-ig-app-id":        appID,
		"X-FB-LSD":           sessionData,
		"X-CSRFToken":        csrfToken,
		"X-Bloks-Version-Id": gqlBloksVersion,
		"x-asbd-id":          gqlAsbdID,
		"cookie":             strings.Join(cookies, "; "),
		"Content-Type":       "application/x-www-form-urlencoded",
		"X-FB-Friendly-Name": polarisAction,
	}
	body := map[string]string{
		"__d":         domain,
		"__a":         apiVersion,
		"__s":         session,
		"__hs":        gqlHiddenState,
		"__req":       requestID,
		"__ccg":       clientCapabilityGrade,
		"__rev":       gqlRolloutHash,
		"__hsi":       sessionInternalID,
		"__dyn":       dynamicFlags,
		"__csr":       clientSessionRnd,
		"__user":      loggedIn,
		"__comet_req": cometRequestID,
		"libav":       appVersion,
		"dpr":         pixelRatio,
		"lsd":         sessionData,
		"jazoest":     jazoest,
		"__spin_r":    gqlRolloutHash,
		"__spin_b":    buildType,
		"__spin_t":    timestamp,
	}
	return headers, body, nil
}

// GetCookieValue returns the value of a named cookie from the extractor's
// HTTP client cookie jar (loaded from private/cookies/instagram.txt).
func GetCookieValue(ctx *models.ExtractorContext, name string) string {
	for _, c := range ctx.HTTPClient.Cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// GetNativeStory fetches a story via Instagram's private mobile API
// (https://i.instagram.com/api/v1/media/<id>/info/) using the session
// cookies loaded from private/cookies/instagram.txt.
func GetNativeStory(ctx *models.ExtractorContext) (*models.Media, error) {
	sessionID := GetCookieValue(ctx, "sessionid")
	if sessionID == "" {
		return nil, fmt.Errorf("no sessionid cookie available")
	}
	csrf := GetCookieValue(ctx, "csrftoken")
	dsUID := GetCookieValue(ctx, "ds_user_id")

	apiURL := "https://i.instagram.com/api/v1/media/" + ctx.ContentID + "/info/"

	cookieStr := "sessionid=" + sessionID
	if csrf != "" {
		cookieStr += "; csrftoken=" + csrf
	}
	if dsUID != "" {
		cookieStr += "; ds_user_id=" + dsUID
	}

	nativeHeaders := map[string]string{
		"User-Agent":  "Instagram 275.0.0.27.98 Android (33/13; 420dpi; 1080x2400; samsung; SM-G991B; o1s; exynos2100; en_US; 458229258)",
		"X-IG-App-ID": "936619743392459",
		"Cookie":      cookieStr,
	}
	if csrf != "" {
		nativeHeaders["X-CSRFToken"] = csrf
	}

	resp, err := ctx.Fetch(
		http.MethodGet,
		apiURL,
		&networking.RequestParams{
			Headers: nativeHeaders,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get response: %s", resp.Status)
	}

	var info MediaInfoResponse
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	if err := decoder.Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(info.Items) == 0 {
		return nil, fmt.Errorf("no items in response")
	}

	result := info.Items[0]
	media := ctx.NewMedia()
	item := media.NewItem()

	switch {
	case len(result.VideoVersions) > 0:
		video := GetBestVideoVersion(result.VideoVersions)
		item.AddFormats(&models.MediaFormat{
			FormatID:   "video",
			Type:       database.MediaTypeVideo,
			URL:        []string{video.URL},
			VideoCodec: database.MediaCodecAvc,
			AudioCodec: database.MediaCodecAac,
			Width:      int32(video.Width),
			Height:     int32(video.Height),
		})
	case result.ImageVersions != nil && len(result.ImageVersions.Candidates) > 0:
		image := GetBestCandidate(result.ImageVersions.Candidates)
		item.AddFormats(&models.MediaFormat{
			Type:     database.MediaTypePhoto,
			FormatID: "photo",
			URL:      []string{image.URL},
		})
	default:
		return nil, fmt.Errorf("no video or image found in story")
	}

	return media, nil
}

// GetDDInstaMedia fetches media via d.ddinstagram.com which proxies Instagram
// embed pages from a different IP, bypassing Instagram's anonymous request blocks.
// It follows the 302 redirect for /p/ID/1, /p/ID/2, ... until a non-redirect
// or 404 is returned, collecting each resolved CDN URL.
func GetDDInstaMedia(ctx *models.ExtractorContext) (*models.Media, error) {
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // do NOT follow; we want the Location header
		},
	}

	media := ctx.NewMedia()

	for idx := 1; idx <= 10; idx++ {
		mediaURL := fmt.Sprintf(
			"https://d.ddinstagram.com/p/%s/%d",
			ctx.ContentID,
			idx,
		)
		req, err := http.NewRequestWithContext(ctx.Context, http.MethodGet, mediaURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build ddinstagram request: %w", err)
		}
		req.Header.Set("User-Agent", webHeaders["User-Agent"])

		resp, err := httpClient.Do(req)
		if err != nil {
			if idx == 1 {
				return nil, fmt.Errorf("ddinstagram request failed: %w", err)
			}
			break
		}
		resp.Body.Close()

		// 404 or anything other than a redirect means no more items
		if resp.StatusCode == http.StatusNotFound {
			break
		}
		if resp.StatusCode != http.StatusFound &&
			resp.StatusCode != http.StatusMovedPermanently &&
			resp.StatusCode != http.StatusSeeOther &&
			resp.StatusCode != http.StatusTemporaryRedirect &&
			resp.StatusCode != http.StatusPermanentRedirect {
			break
		}

		location := resp.Header.Get("Location")
		if location == "" {
			break
		}

		item := media.NewItem()
		// Determine type by extension in the CDN URL
		lowerLoc := strings.ToLower(location)
		if strings.Contains(lowerLoc, ".mp4") {
			item.AddFormats(&models.MediaFormat{
				FormatID:   "video",
				Type:       database.MediaTypeVideo,
				VideoCodec: database.MediaCodecAvc,
				AudioCodec: database.MediaCodecAac,
				URL:        []string{location},
			})
		} else {
			item.AddFormats(&models.MediaFormat{
				FormatID: "image",
				Type:     database.MediaTypePhoto,
				URL:      []string{location},
			})
		}
	}

	if len(media.Items) == 0 {
		return nil, fmt.Errorf("no media found via ddinstagram")
	}
	return media, nil
}

func GetBestCandidate(candidates []*Candidates) *Candidates {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, candidate := range candidates {
		if candidate.Width > best.Width {
			best = candidate
		}
	}
	return best
}

func GetBestVideoVersion(versions []*VideoVersions) *VideoVersions {
	if len(versions) == 0 {
		return nil
	}
	best := versions[0]
	for _, version := range versions {
		if version.Width > best.Width {
			best = version
		}
	}
	return best
}
