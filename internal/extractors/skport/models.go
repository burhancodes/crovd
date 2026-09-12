package skport

type AuthResponse struct {
	Code int      `json:"code"`
	Data AuthData `json:"data"`
}

type AuthData struct {
	Token string `json:"token"`
}

type ItemResponse struct {
	Code int      `json:"code"`
	Data ItemData `json:"data"`
}

type ItemData struct {
	ItemInfo ItemInfo `json:"itemInfo"`
}

type ItemInfo struct {
	Item Item `json:"item"`
}

type Item struct {
	ID       string         `json:"id"`
	UserID   string         `json:"userId"`
	GameID   int            `json:"gameId"`
	ViewKind int            `json:"viewKind"`
	Caption  []*CaptionItem `json:"caption"`
	Album    []*AlbumEntry  `json:"album"`
	Cover    *ImageInfo     `json:"cover"`
	Document *Document      `json:"document"`
}

type CaptionItem struct {
	Kind string       `json:"kind"`
	Text *CaptionText `json:"text,omitempty"`
}

type CaptionText struct {
	Text string `json:"text"`
}

type AlbumEntry struct {
	BlockID string     `json:"blockId"`
	Image   *ImageInfo `json:"image"`
}

type ImageInfo struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Width  string `json:"width"`
	Height string `json:"height"`
	Format string `json:"format"`
	Kind   string `json:"kind"` // "static" or "animated"
}

type Document struct {
	ID       string            `json:"id"`
	BlockIDs []string          `json:"blockIds"`
	BlockMap map[string]*Block `json:"blockMap"`
}

type Block struct {
	ID    string     `json:"id"`
	Kind  string     `json:"kind"`
	Image *ImageInfo `json:"image,omitempty"`
}
