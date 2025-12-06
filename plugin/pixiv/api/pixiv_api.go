package api

import (
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"sort"
)

type PixivAPI struct {
	Client *Client
	Token  *TokenStore
}

func NewPixivAPI(refreshToken string, proxy string) *PixivAPI {
	c := NewClient(proxy)
	return &PixivAPI{
		Client: c,
		Token:  NewTokenStore(refreshToken, c),
	}
}

func (p *PixivAPI) FetchPixivByPID(pid int64) (*model.IllustCache, error) {
	url := fmt.Sprintf("https://app-api.pixiv.net/v1/illust/detail?illust_id=%d", pid)
	accessToken, err := p.Token.GetAccessToken()
	if err != nil {
		return nil, err
	}
	rawData, err := p.Client.SearchPixivIllustrations(accessToken, url)
	if err != nil {
		return nil, err
	}
	if rawData == nil || rawData.Illust == nil {
		return nil, fmt.Errorf("pixiv 返回数据为空或结构不匹配")
	}
	illust := *rawData.Illust

	return convertToIllustCache(illust)
}

func (p *PixivAPI) FetchPixivByUser(uid int64, limit int, pids []int64) ([]model.IllustCache, error) {
	url := fmt.Sprintf("https://app-api.pixiv.net/v1/user/illusts?user_id=%d&type=illust", uid)

	excludeCache := make(map[int64]struct{})

	/*	var pids []int64
		err := db.Model(&model.SentImage{}).Where("group_id = ?", gid).Pluck("pid", &pids).Error
		if err != nil {
			return nil, err
		}*/
	for _, pid := range pids {
		excludeCache[pid] = struct{}{}
	}
	return p.fetchPixivCommon(url, limit, nil, excludeCache)
}

func (p *PixivAPI) FetchPixivRecommend(limit int) ([]model.IllustCache, error) {
	firstURL := "https://app-api.pixiv.net/v1/illust/recommended?filter=for_ios"
	return p.fetchPixivCommon(firstURL, limit, nil, nil) // 不做R18过滤，不排缓存
}

func (p *PixivAPI) FetchPixivIllusts(keyword string, isR18Req bool, limit int, cachedIds []int64) ([]model.IllustCache, error) {
	/*	// 只取当前 keyword 的缓存
		var cachedIds []int64
		if err := db.Model(&IllustCache{}).
			Where("keyword = ?", keyword).
			Pluck("pid", &cachedIds).Error; err != nil {
			return nil, err
		}*/

	cachedMap := make(map[int64]struct{}, len(cachedIds))
	if len(cachedIds) > 0 {
		for _, id := range cachedIds {
			cachedMap[id] = struct{}{}
		}
	}

	firstURL := buildPixivSearchURL(keyword)
	return p.fetchPixivCommon(firstURL, limit, &isR18Req, cachedMap, keyword)
}

func (p *PixivAPI) GetIllustsByKeyword(keyword string, limit int, cachedIllust []model.IllustCache, cached []int64) ([]model.IllustCache, error) {

	r18Req := IsR18(keyword)
	keyword = RemoveR18Keywords(keyword)

	// 如果查到了，直接返回
	if len(cachedIllust) == limit {
		return cachedIllust, nil
	}

	// 设置一个保底的关键词
	if keyword == "" && r18Req {
		keyword = "R-18"
	}

	// 计算还需要几张图片
	needed := 0
	if len(cachedIllust) < limit {
		needed = limit - len(cachedIllust)
	}

	fmt.Printf("从数据库读到%d,还需要下载%d\n", len(cachedIllust), needed)
	// 缓存没数据 -> 调用Pixiv API拉取
	pixivResults, err := p.FetchPixivIllusts(keyword, r18Req, needed, cached)
	if err != nil && len(cachedIllust) == 0 {
		return nil, err
	}

	// 如果Pixiv也没查到直接返回空
	if len(pixivResults) == 0 && len(cachedIllust) == 0 {
		return nil, fmt.Errorf("这个关键词可能没有找到符合条件的图片或出现未知错误")
	}

	if len(cachedIllust) > 0 && len(pixivResults) == 0 {
		fmt.Println("http没有找到图片")
		return cachedIllust, nil
	}

	pixivResults = append(pixivResults, cachedIllust...)

	if len(pixivResults) >= limit {
		pixivResults = pixivResults[:limit]
	}

	fmt.Println("预计发送", len(pixivResults), "张图片")

	return pixivResults, nil
}

func (p *PixivAPI) fetchPixivCommon(
	firstURL string,
	limit int,
	isR18Req *bool,
	excludeCache map[int64]struct{},
	keywords ...string,
) ([]model.IllustCache, error) {

	accessToken, err := p.Token.GetAccessToken()
	if err != nil {
		return nil, err
	}

	// 高质量图（≥1000）
	high := make([]model.IllustCache, 0, limit)
	// 低质量图（<1000）
	low := make([]model.IllustCache, 0, limit)

	seen := make(map[int64]struct{})
	url := firstURL

	for url != "" {
		rawData, err := p.Client.SearchPixivIllustrations(accessToken, url)
		if err != nil {
			return nil, err
		}

		for _, raw := range rawData.Illusts {

			// 去重
			if _, ok := seen[raw.Id]; ok {
				continue
			}
			if excludeCache != nil {
				if _, ok := excludeCache[raw.Id]; ok {
					continue
				}
			}
			seen[raw.Id] = struct{}{}

			// 转换
			ill, err := convertToIllustCache(raw)
			if err != nil {
				continue
			}
			if len(keywords) > 0 {
				ill.Keyword = keywords[0]
			}

			// R18过滤
			if isR18Req != nil && ill.R18 != *isR18Req {
				continue
			}

			// 判断高质量
			if ill.Bookmarks >= 1000 {
				high = append(high, *ill)
				// 高质量够了就直接返回
				if len(high) >= limit {
					return high[:limit], nil
				}
			} else {
				// 低质量池还没满 → 接受
				if len(low) <= limit {
					low = append(low, *ill)
				}
				// 低质量够 limit 就不再放入，避免爆炸增长
			}
		}

		// 下一页继续
		url = rawData.NextUrl
	}

	// ==== 翻页结束：如果高质量不足，就从低质量中挑收藏最多的 ====

	// 低质量排序（按收藏数倒序）
	sort.Slice(low, func(i, j int) bool {
		return low[i].Bookmarks > low[j].Bookmarks
	})

	// 用低质量中的高质量去补齐不足的图
	all := append(high, low...)
	if len(all) > limit {
		all = all[:limit]
	}

	return all, nil
}
