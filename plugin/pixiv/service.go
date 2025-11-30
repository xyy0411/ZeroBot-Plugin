package pixiv

import (
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/api"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/cache"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/proxy"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"sync"
)

var cacheFilling sync.Map

// Service 用于封装整个 Pixiv 模块的依赖与接口
type Service struct {
	DB    *cache.DB
	API   *api.PixivAPI
	Proxy *proxy.Manager
	// 内部任务锁：限制每个群同一时间只能执行一个请求
	taskMu sync.Mutex
	tasks  map[int64]*taskState

	// 并发控制
	DownloadWorkers int
	SendWorkers     int
}

type taskState struct {
	Running bool
}

func NewService(db *cache.DB, api *api.PixivAPI, proxy *proxy.Manager) *Service {
	return &Service{
		DB:              db,
		API:             api,
		Proxy:           proxy,
		tasks:           make(map[int64]*taskState),
		DownloadWorkers: 4,
		SendWorkers:     2,
	}
}

func (s *Service) Acquire(userID int64) bool {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	t, ok := s.tasks[userID]
	if ok && t.Running {
		return false
	}

	if !ok {
		t = &taskState{}
		s.tasks[userID] = t
	}
	t.Running = true
	return true
}

func (s *Service) Release(userID int64) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	if t, ok := s.tasks[userID]; ok {
		t.Running = false
	}
	delete(s.tasks, userID)
}

func (s *Service) SendIllusts(ctx *zero.Ctx, illusts []model.IllustCache, gid int64) {
	downloadSem := make(chan struct{}, s.DownloadWorkers)
	type DLResult struct {
		Ill model.IllustCache
		Img []byte
		Err error
	}

	results := make(chan DLResult, len(illusts))

	// 并发下载
	for _, ill := range illusts {
		ill1 := ill
		downloadSem <- struct{}{}

		go func() {
			defer func() { <-downloadSem }()

			img, err := s.API.Client.FetchPixivImage(ill1, ill1.OriginalURL, true)
			results <- DLResult{Ill: ill1, Img: img, Err: err}
		}()
	}

	// 接收并发送（顺序）
	for range illusts {
		res := <-results

		// 下载失败
		if res.Err != nil {
			ctx.SendChain(message.Text("下载失败: ", res.Err))
			continue
		}

		// 发送消息（顺序执行，不开 goroutine）
		ctx.SendChain(
			message.Text(
				"PID:", res.Ill.PID,
				"\n标题:", res.Ill.Title,
				"\n画师:", res.Ill.AuthorName,
				"\ntag:", res.Ill.Tags,
				"\n收藏数:", res.Ill.Bookmarks,
				"\n浏览数:", res.Ill.TotalView,
				"\n时间:", res.Ill.CreateDate,
			),
			message.ImageBytes(res.Img),
		)

		// 图片已完全用完 → 这里立刻释放（最关键）
		res.Img = nil

		// 写入发送记录
		s.DB.Create(&model.SentImage{
			GroupID: gid,
			PID:     res.Ill.PID,
		})
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

		sendedcache, err := service.DB.GetSentPictureIDs(gid)
		if err != nil {
			fmt.Println("后台补充缓存失败:", err)
			return
		}
		newIllusts, err := service.API.FetchPixivIllusts(keyword, r18Req, fetchCount, sendedcache)
		if err != nil {
			fmt.Println("后台补充缓存失败:", err)

			return
		}

		if len(newIllusts) == 0 {
			fmt.Println("后台补充缓存：没有新图")
			return
		}

		for _, illust := range newIllusts {
			fmt.Println("后台补充缓存：", illust.PID)
			err = s.DB.Create(&illust).Error
			fmt.Println("err:", err)
		}

		fmt.Printf("后台成功补充 %d 张图片到关键词 %s 缓存\n", len(newIllusts), keyword)
	}()
}
