package pixiv

import (
	"encoding/json"
	"errors"
	"github.com/jinzhu/gorm"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func NewClient() *http.Client {
	return defaultClient
}

func ProcessError(err error) bool {
	if errors.Is(err, io.EOF) {
		autoSwitchConcurrent()
		return true
	}
	return false
}

func CountIllustsSmart(gid int64, keyword string, r18Req bool) (int64, error) {
	var count int64

	query := buildIllustQuery(gid, keyword, r18Req)
	err := query.Count(&count).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	return count, nil
}

// FindIllustsSmart 查找缓存（先 keyword，再 tags）
// - keyword: 用户输入关键词
// - r18Req: 是否允许 R18
// - limit: 需要返回的数量
// - gid: 用于排除已发送的 group id（SentImage 表）
// 返回：尽量返回最多 limit 条结果
// buildIllustQuery 封装基础查询逻辑（keyword + tags + 已发送过滤 + R18过滤）
func FindIllustsSmart(gid int64, keyword string, limit int, r18Req bool) ([]IllustCache, error) {
	var illusts []IllustCache

	query := buildIllustQuery(gid, keyword, r18Req)
	err := query.Limit(limit).Find(&illusts).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return illusts, nil
}

// buildIllustQuery 封装基础查询逻辑（keyword + tags + 已发送过滤 + R18过滤）
func buildIllustQuery(gid int64, keyword string, r18Req bool) *gorm.DB {
	sub := db.Model(&SentImage{}).
		Where("group_id = ?", gid).
		Select("pid").
		SubQuery()

	query := db.Model(&IllustCache{}).
		Where("pid NOT IN (?)", sub).
		Where("(keyword = ?) OR (keyword <> ? AND tags LIKE ?)", keyword, keyword, "%"+keyword+"%")

	if !r18Req {
		query = query.Where("r18 = ?", false)
	}
	return query
}

func removeR18Keywords(keyword string) string {
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

func requiresNonR18(keyword string) bool {
	if keyword == "" {
		return true // 默认情况下过滤R-18
	}
	lower := strings.ToLower(keyword)
	nonR18Keywords := []string{"非r18", "非r-18", "safe", "全年龄", "健全"}
	for _, kw := range nonR18Keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// 如果关键词中不包含R-18相关词汇，则默认过滤R-18
	return false
}

func isR18(s string) bool {
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

func BuildPixivSearchURL(keyword string) string {

	baseURL := &url.URL{
		Scheme: "https",
		Host:   "app-api.pixiv.net",
		Path:   "/v1/search/illust",
	}

	params := url.Values{}
	params.Set("word", keyword)
	params.Set("sort", "popular_desc")
	// 严格匹配 exact_match_for_tags
	// 标题简介有相同的 title_and_caption
	// 宽松匹配 partial_match_for_tags
	// 暂时使用宽松匹配
	params.Set("search_target", "partial_match_for_tags")
	//params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("order", "date_desc")
	params.Set("filter", "for_ios")
	// params.Set("bookmark_num_min", "1000")

	baseURL.RawQuery = params.Encode()
	return baseURL.String()
}

func hasR18Tag(tags []string) bool {
	for _, tag := range tags {
		if isR18(tag) {
			return true
		}
	}
	return false
}

func convertToIllustCache(raw IllustsEntity) (IllustCache, error) {
	var tagNames []string
	for _, tag := range raw.Tags {
		tagNames = append(tagNames, tag.Name)
	}

	jsonTags, err := json.Marshal(tagNames)
	if err != nil {
		return IllustCache{}, err
	}

	illust := IllustCache{
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
