package pixiv

import (
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/api"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/cache"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/proxy"
	"sync"
)

var cacheFilling sync.Map

// Service 用于封装整个 Pixiv 模块的依赖与接口
type Service struct {
	DB    *cache.DB
	API   *api.PixivAPI
	Proxy *proxy.Manager
}

func NewService(db *cache.DB, api *api.PixivAPI, proxy *proxy.Manager) *Service {
	return &Service{
		DB:    db,
		API:   api,
		Proxy: proxy,
	}
}

func (s *Service) BackgroundCacheFiller(keyword string, minCache int, r18Req bool, fetchCount int, gid int64) {
	if _, loaded := cacheFilling.LoadOrStore(keyword, struct{}{}); loaded {
		fmt.Println("已有后台任务在补缓存:", keyword)
		return
	}

	go func() {
		defer cacheFilling.Delete(keyword)

		count, err := s.DB.CountIllustsSmart(gid, keyword, r18Req)
		if err != nil {
			fmt.Println("查询数据库发生错误:", err)
			return
		}

		if count >= int64(minCache) {
			fmt.Println("缓存足够，无需补充:", keyword)
			return
		}

		fmt.Printf("后台补充关键词 %s, 数量 %d\n", keyword, fetchCount)

		cached := s.DB.FindCached(keyword)
		newIllusts, err := service.API.FetchPixivIllusts(keyword, r18Req, fetchCount, cached)
		if err != nil {
			fmt.Println("后台补充缓存失败:", err)
			return
		}

		if len(newIllusts) == 0 {
			fmt.Println("后台补充缓存：没有新图")
			return
		}
		for _, illust := range newIllusts {
			_ = s.DB.Create(illust).Error
		}

		fmt.Printf("后台成功补充 %d 张图片到关键词 %s 缓存\n", len(newIllusts), keyword)
	}()
}
