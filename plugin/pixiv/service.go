package pixiv

import (
	"errors"
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/api"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/cache"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"github.com/FloatTech/floatbox/file"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
)

var cacheFilling sync.Map

// Service 用于封装整个 Pixiv 模块的依赖与接口
type Service struct {
	DB  *cache.DB
	API *api.PixivAPI
	// 内部任务锁：限制每个人同一时间只能执行一个请求
	taskMu sync.Mutex
	tasks  map[int64]*taskState

	// 并发控制
	DownloadWorkers int
	SendWorkers     int
}

type taskState struct {
	Running bool
}

func NewService(db *cache.DB, api *api.PixivAPI) *Service {
	return &Service{
		DB:              db,
		API:             api,
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

func (s *Service) SendIllusts(ctx *zero.Ctx, illusts []model.IllustCache) {
	downloadSem := make(chan struct{}, s.DownloadWorkers)
	type DLResult struct {
		Ill      model.IllustCache
		ImgPaths []string
		Err      error
	}

	gid := ctx.Event.GroupID
	if gid == 0 {
		gid = -ctx.Event.UserID
	}

	results := make(chan DLResult, len(illusts))

	// 并发下载
	for _, ill := range illusts {
		ill1 := ill
		downloadSem <- struct{}{}

		go func() {
			defer func() { <-downloadSem }()

			d := DLResult{
				Ill: ill1,
			}

			if ill1.PageCount > 1 && len(illusts) == 1 {

				type pageResult struct {
					index int
					path  string
					err   error
				}

				pageCh := make(chan pageResult, ill1.PageCount)
				var wg sync.WaitGroup

				pageSem := make(chan struct{}, s.DownloadWorkers)

				for i := 0; int64(i) < ill1.PageCount; i++ {
					wg.Add(1)
					pageSem <- struct{}{}

					go func(page int) {
						defer wg.Done()
						defer func() { <-pageSem }()

						u := api.ModifyPageGeneric(ill1.OriginalURL, page)
						img, err := s.API.Client.FetchPixivImage(ill1, u)
						if err != nil {
							pageCh <- pageResult{
								index: page,
								err:   err,
							}
							return
						}
						imagePath := buildPixivImagePath(ill1.PID, page+1, u)
						if writeErr := os.WriteFile(imagePath, img, 0644); writeErr != nil {
							pageCh <- pageResult{
								index: page,
								err:   writeErr,
							}
							return
						}

						pageCh <- pageResult{
							index: page,
							path:  imagePath,
							err:   err,
						}
					}(i)
				}

				// 关闭 channel
				go func() {
					wg.Wait()
					close(pageCh)
				}()

				// 预分配，保证顺序
				d.ImgPaths = make([]string, ill1.PageCount)

				for r := range pageCh {
					if r.err != nil && d.Err == nil {
						d.Err = r.err
					}
					d.ImgPaths[r.index] = r.path
				}

			} else {
				img, err := s.API.Client.FetchPixivImage(ill1, ill1.OriginalURL)
				if err == nil {
					imagePath := buildPixivImagePath(ill1.PID, 0, ill1.OriginalURL)
					if writeErr := os.WriteFile(imagePath, img, 0644); writeErr != nil {
						d.Err = writeErr
					} else {
						d.ImgPaths = append(d.ImgPaths, imagePath)
					}
				} else {
					d.Err = err
				}
			}

			fmt.Println("下载图片完成：", ill1.PID)
			results <- d
		}()

	}

	// 发送（顺序）
	for range illusts {
		res := <-results

		if res.Err != nil {
			cleanupPixivImages(res.ImgPaths)
			var httpErr *api.HTTPStatusError
			if errors.As(res.Err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				if err := s.DB.DeleteIllustByPID(res.Ill.PID); err != nil {
					ctx.SendChain(message.Text("清理已失效图片失败: ", err))
				} else {
					ctx.SendChain(message.Text("图片已被删除，已移除缓存，PID: ", res.Ill.PID))
				}
			} else {
				ctx.SendChain(message.Text("下载失败: ", res.Err))
			}

			continue
		}

		var msg message.Message
		msg = append(msg, message.Text(
			"PID:", res.Ill.PID,
			"\n标题:", res.Ill.Title,
			"\n画师:", res.Ill.AuthorName,
			"\ntag:", res.Ill.Tags,
			"\n收藏数:", res.Ill.Bookmarks,
			"\n浏览数:", res.Ill.TotalView,
			"\n发布时间:", res.Ill.CreateDate,
		))
		for i := 0; i < len(res.ImgPaths); i++ {
			if res.ImgPaths[i] == "" {
				continue
			}
			msg = append(msg, message.Image("file:///"+filepath.ToSlash(res.ImgPaths[i])))
			msg = append(msg, message.Image(res.ImgPaths[i]))
		}

		ctx.Send(msg)

		cleanupPixivImages(res.ImgPaths)

		s.DB.Create(&model.SentImage{
			GroupID: gid,
			PID:     res.Ill.PID,
		})
	}
}

func buildPixivImagePath(pid int64, index int, rawURL string) string {
	ext := pixivImageExt(rawURL)
	if index > 0 {
		return filepath.Join(file.BOTPATH, "data", "pixiv", fmt.Sprintf("%d-%d%s", pid, index, ext))
	}
	return filepath.Join(file.BOTPATH, "data", "pixiv", fmt.Sprintf("%d%s", pid, ext))
}

func pixivImageExt(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ".jpg"
	}
	ext := path.Ext(parsed.Path)
	if ext == "" {
		return ".jpg"
	}
	return ext
}

func cleanupPixivImages(paths []string) {
	for _, imagePath := range paths {
		if imagePath == "" {
			continue
		}
		_ = os.Remove(imagePath)
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

		sendedcache, err := s.DB.GetSentPictureIDs(gid)
		if err != nil {
			fmt.Println("后台补充缓存失败:", err)
			return
		}
		s1, err := s.DB.GetIllustIDsByKeyword(keyword)
		sendedcache = append(sendedcache, s1...)
		newIllusts, err := s.API.FetchPixivIllusts(keyword, r18Req, fetchCount, sendedcache)
		if err != nil {
			fmt.Println("后台补充缓存失败:", err)

			return
		}

		if len(newIllusts) == 0 {
			fmt.Println("后台补充缓存：没有新图")
			return
		}

		for _, illust := range newIllusts {
			s.DB.Create(&illust)
			fmt.Println("后台补充缓存：", illust.PID)
		}

		fmt.Printf("后台成功补充 %d 张图片到关键词 %s 缓存\n", len(newIllusts), keyword)
	}()
}
