package pixiv

import (
	"fmt"
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
}

func NewTokenStore() *TokenStore {
	return &TokenStore{}
}

func (t *TokenStore) GetAccessToken() (string, error) {

	// 1. access token 还有效，直接用
	if time.Now().After(t.ExpiresAt) && t.AccessToken != "" {
		return t.AccessToken, nil
	}

	if t.AccessToken == "" {
		var t1 RefreshToken
		if err := db.First(&t1).Error; err != nil {
			return "", err
		}
		t.RefreshToken = t1.Token
	}
	// 2. 否则用 refresh token 刷新
	newToken, err := RefreshPixivAccessToken(t.RefreshToken)
	if err != nil {
		return "", err
	}

	t.AccessToken = newToken.AccessToken
	t.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	fmt.Println("AccessToken:", t.AccessToken)
	return newToken.AccessToken, nil
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
