package pixiv

import (
	"net/url"
	"strings"
)

func removeR18Keywords(keyword string) string {
	if keyword == "" {
		return keyword
	}

	words := strings.Fields(keyword) // 按空格分割成单词
	var result []string

	for _, word := range words {
		lowerWord := strings.ToLower(word)
		// 只删除完全匹配的R-18关键词
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
	return !isR18(keyword)
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
	keyword += " 1000users入り"
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

	baseURL.RawQuery = params.Encode()
	return baseURL.String()
}

// ToIllustSummary 提取有用的字段
func ToIllustSummary(illust IllustsEntity) IllustSummary {
	// 提取标签名称
	var tags []string
	for _, tag := range illust.Tags {
		tags = append(tags, tag.Name)
	}

	return IllustSummary{
		PID:            illust.Id,
		Title:          illust.Title,
		Type:           illust.Type,
		ImageUrl:       illust.ImageUrls.Medium,
		AuthorName:     illust.User.Name,
		AuthorID:       illust.User.Id,
		Tags:           tags,
		CreateDate:     illust.CreateDate,
		PageCount:      illust.PageCount,
		TotalView:      illust.TotalView,
		TotalBookmarks: illust.TotalBookmarks,
		IsBookmarked:   illust.IsBookmarked,
		IsAI:           illust.IllustAiType > 0,
	}
}

// ToIllustSummaries 批量转换函数
func ToIllustSummaries(illusts []IllustsEntity) []IllustSummary {
	summaries := make([]IllustSummary, len(illusts))
	for i, illust := range illusts {
		summaries[i] = ToIllustSummary(illust)
	}
	return summaries
}
