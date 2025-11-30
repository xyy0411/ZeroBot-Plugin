package api

import (
	"encoding/json"
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"net/url"
	"strings"
)

func replaceDomain(originalURL, newDomain string) (string, error) {
	u, err := url.Parse(originalURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	u.Host = newDomain

	return u.String(), nil
}

func RemoveR18Keywords(keyword string) string {
	if keyword == "" {
		return keyword
	}

	words := strings.Fields(keyword) // 按空格分割成单词
	var result []string

	for _, word := range words {
		lowerWord := strings.ToLower(word)
		// 只删除完全匹配的R-18 关键词
		if lowerWord != "r-18" && lowerWord != "r18" && lowerWord != "r_18" {
			result = append(result, word)
		}
	}

	return strings.Join(result, " ")
}

func buildPixivSearchURL(keyword string) string {

	baseURL := &url.URL{
		Scheme: "https",
		Host:   "app-api.pixiv.net",
		Path:   "/v1/search/illust",
	}

	params := url.Values{}
	params.Set("word", keyword)
	// 严格匹配 exact_match_for_tags
	// 标题简介有相同的 title_and_caption
	// 宽松匹配 partial_match_for_tags
	// 暂时使用宽松匹配
	params.Set("search_target", "partial_match_for_tags")
	params.Set("sort", "popular_desc")
	//params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("order", "date_desc")
	params.Set("filter", "for_android")
	// params.Set("filter", "for_ios")
	// params.Set("bookmark_num_min", "1000")

	baseURL.RawQuery = params.Encode()
	return baseURL.String()
}

func hasR18Tag(tags []string) bool {
	for _, tag := range tags {
		if IsR18(tag) {
			return true
		}
	}
	return false
}

func IsR18(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	r18Keywords := []string{"r-18", "r18", "r_18"}
	for _, keyword := range r18Keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func convertToIllustCache(raw model.IllustsEntity) (*model.IllustCache, error) {
	var tagNames []string
	for _, tag := range raw.Tags {
		tagNames = append(tagNames, tag.Name)
	}

	jsonTags, err := json.Marshal(tagNames)
	if err != nil {
		return nil, err
	}

	illust := &model.IllustCache{
		PID:        raw.Id,
		UID:        raw.User.Id,
		Keyword:    tagNames[0], // 默认为第1个标签后续在其他函数里自定义
		Title:      raw.Title,
		AuthorName: raw.User.Name,
		ImageURL:   raw.ImageUrls.Large,
		R18:        (raw.XRestrict == 1) || hasR18Tag(tagNames),
		Bookmarks:  raw.TotalBookmarks,
		TotalView:  raw.TotalView,
		CreateDate: raw.CreateDate,
		PageCount:  raw.PageCount,
		Tags:       jsonTags,
	}

	originalImageURL := raw.MetaSinglePage.OriginalImageUrl
	if originalImageURL == "" && len(raw.MetaPages) > 0 {
		originalImageURL = raw.MetaPages[0].ImageURLs.Original
	}

	illust.OriginalURL = originalImageURL

	return illust, nil
}
