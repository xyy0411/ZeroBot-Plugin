package pixiv

import (
	"bytes"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"time"
)

type TokenStore struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int64     `json:"expires_in"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"-"`
}

type IllustSummary struct {
	PID            int64    // 作品ID
	Title          string   // 作品标题
	Type           string   // 作品类型
	OriginalUrl    string   // 原图url
	ImageUrl       string   // 主要图片URL
	AuthorName     string   // 作者名称
	AuthorID       int64    // 作者ID
	Tags           []string // 标签列表
	CreateDate     string   // 创建日期
	PageCount      int64    // 页数
	TotalView      int64    // 总浏览数
	TotalBookmarks int64    // 总收藏数
	IsBookmarked   bool     // 是否已收藏
	IsAI           bool     // 是否为AI作品
	R18            bool     // 是不是18+的
}

func NewTokenStore() *TokenStore {
	var t1 RefreshToken
	if err := db.First(&t1).Error; err != nil {
		log.Error("Error fetching token store from database")
	}
	return &TokenStore{
		RefreshToken: t1.Token,
	}
}

func (t *TokenStore) GetAccessToken() (string, error) {

	// 1. access token 还有效，直接用
	if time.Now().Before(t.ExpiresAt) && t.AccessToken != "" {
		fmt.Println("access_token is expired")
		return t.AccessToken, nil
	}

	// 2. 否则用 refresh token 刷新
	err := t.RefreshPixivAccessToken()
	if err != nil {
		return "", err
	}

	t.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn/2) * time.Second)
	return t.AccessToken, nil
}

// RefreshPixivAccessToken 用 refresh_token 刷新 access_token
func (t *TokenStore) RefreshPixivAccessToken() error {
	endpoint := "https://oauth.secure.pixiv.net/auth/token"

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", "MOBrBDS8blbauoSck0ZfDbtuzpyT")
	data.Set("client_secret", "lsACyCD94FhDUtGTXi3QzcFE2uU1hqtDaKeqrdwj")
	data.Set("refresh_token", t.RefreshToken)

	req, _ := http.NewRequest("POST", endpoint, bytes.NewBufferString(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "PixivAndroidApp/5.0.234 (Android 11; Pixel 5)")

	client := NewClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("刷新失败: %s\nbody: %s", resp.Status, string(body))
	}

	var tokenRes TokenStore
	err = json.Unmarshal(body, &tokenRes)

	t.AccessToken = tokenRes.AccessToken
	t.ExpiresIn = tokenRes.ExpiresIn

	return err
}

type RootEntity struct {
	Illusts         []IllustsEntity `json:"illusts"`
	NextUrl         string          `json:"next_url"`
	SearchSpanLimit int64           `json:"search_span_limit"`
	ShowAi          bool            `json:"show_ai"`
}

type IllustsEntity struct {
	Id                          int64                `json:"id"`
	Title                       string               `json:"title"`
	Type                        string               `json:"type"`
	ImageUrls                   ImageUrlsEntity      `json:"image_urls"`
	Caption                     string               `json:"caption"`
	Restrict                    int64                `json:"restrict"`
	User                        UserEntity           `json:"user"`
	Tags                        []TagsEntity         `json:"tags"`
	Tools                       []interface{}        `json:"tools"`
	CreateDate                  string               `json:"create_date"`
	PageCount                   int64                `json:"page_count"`
	Width                       int64                `json:"width"`
	Height                      int64                `json:"height"`
	SanityLevel                 int64                `json:"sanity_level"`
	XRestrict                   int64                `json:"x_restrict"`
	Series                      interface{}          `json:"series"`
	MetaSinglePage              MetaSinglePageEntity `json:"meta_single_page"`
	MetaPages                   []MetaPage           `json:"meta_pages"`
	TotalView                   int64                `json:"total_view"`
	TotalBookmarks              int64                `json:"total_bookmarks"`
	IsBookmarked                bool                 `json:"is_bookmarked"`
	Visible                     bool                 `json:"visible"`
	IsMuted                     bool                 `json:"is_muted"`
	SeasonalEffectAnimationUrls interface{}          `json:"seasonal_effect_animation_urls"`
	EventBanners                interface{}          `json:"event_banners"`
	IllustAiType                int64                `json:"illust_ai_type"`
	IllustBookStyle             int64                `json:"illust_book_style"`
	Request                     interface{}          `json:"request"`
}

type MetaPage struct {
	ImageURLs struct {
		SquareMedium string `json:"square_medium"`
		Medium       string `json:"medium"`
		Large        string `json:"large"`
		Original     string `json:"original"`
	} `json:"image_urls"`
}

type ImageUrlsEntity struct {
	SquareMedium string `json:"square_medium"`
	Medium       string `json:"medium"`
	Large        string `json:"large"`
}

type UserEntity struct {
	Id               int64                  `json:"id"`
	Name             string                 `json:"name"`
	Account          string                 `json:"account"`
	ProfileImageUrls ProfileImageUrlsEntity `json:"profile_image_urls"`
	IsFollowed       bool                   `json:"is_followed"`
	IsAcceptRequest  bool                   `json:"is_accept_request"`
}

type ProfileImageUrlsEntity struct {
	Medium string `json:"medium"`
}

type TagsEntity struct {
	Name           string      `json:"name"`
	TranslatedName interface{} `json:"translated_name"`
}

type MetaSinglePageEntity struct {
	OriginalImageUrl string `json:"original_image_url"`
}
