package pixiv

import (
	"errors"
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/api"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/cache"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/proxy"
	"github.com/FloatTech/floatbox/file"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var defaultKeyword = []string{"萝莉", "御姐", "妹妹", "姐姐"}

var (
	service *Service
)

func init() {
	if file.IsNotExist("data/pixiv") {
		err := os.MkdirAll("data/pixiv", 0775)
		if err != nil {
			panic(err)
		}
	}

	db := cache.NewDB("data/pixiv/pixiv.db")

	var t1 model.RefreshToken
	if err := db.First(&t1).Error; err != nil {
		log.Warning("Fail fetching token store from database")
	}

	pixivAPI := api.NewPixivAPI(t1.Token, "http://127.0.0.1:10809")
	manager := proxy.NewManager(db)
	service = NewService(db, pixivAPI, manager)
}

func init() {

	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "Pixiv 图片搜索",
		Help:             "- [x张]涩图 [关键词]\n- 每日涩图\n- [x张]画师[画师的uid] \n- p站搜图[插画pid] \n- 下载代理<url>\n- 列出/手动切换代理节点：切换代理节点 [编号]\n- 自动切换代理节点\n[]为可忽略项\n可添加多个关键词每个关键词用空格隔开\n默认不发R-18如果要发就加一个R-18关键词",
	})

	engine.OnRegex(`^下载代理*(.+)`, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		url := ctx.State["regex_matched"].([]string)[1]
		err := service.Proxy.DownloadingNode(url)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
		}
		ctx.SendChain(message.Text("代理节点已更新"))
	})

	engine.OnRegex(`^切换代理节点(?:\s*(\d+))?$`, zero.SuperUserPermission).SetBlock(false).Handle(func(ctx *zero.Ctx) {
		idxStr := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
		if idxStr == "" {
			nodes, err := service.Proxy.ListNodes()
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}

			var sb strings.Builder
			sb.WriteString("可选代理节点：\n")
			for i, n := range nodes {
				sb.WriteString(fmt.Sprintf("#%d %s (%s:%s)\n", i+1, n.Name, n.Address, n.Port))
			}
			sb.WriteString("使用 \"切换代理节点 <编号>\" 进行切换")
			ctx.SendChain(message.Text(sb.String()))
			return
		}

		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: 无效编号"))
			return
		}

		msg, err := service.Proxy.SwitchTo(idx)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text(msg))
	})

	engine.OnFullMatch("自动切换代理节点", zero.SuperUserPermission).SetBlock(false).Handle(func(ctx *zero.Ctx) {
		msg, err := service.Proxy.AutoSwitch()
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text(msg))
		ctx.SendChain(message.Text("✅ 自动切换完成"))
	})
	engine.OnRegex(`^设置p站token (.*)`, zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		token := ctx.State["regex_matched"].([]string)[1]
		var refreshToken model.RefreshToken
		refreshToken.User = ctx.Event.UserID
		refreshToken.Token = token
		if err := service.DB.Save(&refreshToken).Error; err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		service.API.Token.RefreshToken = token

		ctx.SendChain(message.Text("Pixiv Token: ", token))
	})

	engine.OnRegex(`^p站搜图(\d+)`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		if !service.Acquire(ctx.Event.UserID) {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("上一个任务还没结束，请稍后再试"))
			return
		}
		defer service.Release(ctx.Event.UserID)

		rawPID := ctx.State["regex_matched"].([]string)[1]
		pid, err := strconv.ParseInt(rawPID, 10, 64)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		illust, err := service.API.FetchPixivByPID(pid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		img, usedFallback, err1 := service.API.Client.FetchPixivImage(*illust, illust.OriginalURL)
		if err1 != nil {
			var httpErr *api.HTTPStatusError
			if errors.As(err1, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				_ = service.DB.DeleteIllustByPID(illust.PID)
			}
			ctx.SendChain(message.Text("ERROR: ", err1))
			if usedFallback {
				service.triggerAutoSwitch()
			}
			return
		}
		if usedFallback {
			service.triggerAutoSwitch()
		}
		// tags的类型是json格式所以就不设置keyword了
		_ = service.DB.Create(illust)
		fmt.Println("获取", illust.PID, "成功，准备发送！", float64(len(img))/1024/1024, "mb")
		ctx.SendChain(message.Text(
			"PID:", illust.PID,
			"\n标题:", illust.Title,
			"\n画师:", illust.AuthorName,
			"\ntag:", illust.Tags,
			"\n收藏数:", illust.Bookmarks,
			"\n预览数:", illust.TotalView,
			"\n发布时间:", illust.CreateDate,
		), message.ImageBytes(img))
		gid := ctx.Event.GroupID
		if gid == 0 {
			gid = -ctx.Event.UserID
		}
		service.DB.Create(&model.SentImage{
			GroupID: gid,
			PID:     illust.PID,
		})
	})

	engine.OnRegex(`^(\d+)?张?画师(\d+)`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		if !service.Acquire(ctx.Event.UserID) {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("上一个任务还没结束，请稍后再试"))
			return
		}
		defer service.Release(ctx.Event.UserID)

		limit := ctx.State["regex_matched"].([]string)[1]
		if limit == "" {
			limit = "1"
		}
		rawUid := ctx.State["regex_matched"].([]string)[2]
		limitInt, err := strconv.Atoi(limit)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		uid, err := strconv.ParseInt(rawUid, 10, 64)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		pictureIDs, err := service.DB.GetSentPictureIDs(ctx.Event.GroupID)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		illustInfos, err := service.API.FetchPixivByUser(uid, limitInt, pictureIDs)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		if len(illustInfos) == 0 {
			ctx.SendChain(message.Text("没有找到图片"))
			return
		}
		for _, illust := range illustInfos {

			_ = service.DB.Create(illust)

			img, usedFallback, err1 := service.API.Client.FetchPixivImage(illust, illust.OriginalURL)
			if err1 != nil {
				var httpErr *api.HTTPStatusError
				if errors.As(err1, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
					_ = service.DB.DeleteIllustByPID(illust.PID)
				}
				ctx.SendChain(message.Text("ERROR: ", err1))
				if usedFallback {
					service.triggerAutoSwitch()
				}
				continue
			}
			if usedFallback {
				service.triggerAutoSwitch()
			}
			fmt.Println("获取", illust.PID, "成功，准备发送！", float64(len(img))/1024/1024, "mb")
			if msgID := ctx.SendChain(message.Text(
				"PID:", illust.PID,
				"\n标题:", illust.Title,
				"\n画师:", illust.AuthorName,
				"\ntag:", illust.Tags,
				"\n收藏数:", illust.Bookmarks,
				"\n预览数:", illust.TotalView,
				"\n发布时间:", illust.CreateDate,
			), message.ImageBytes(img)); msgID.ID() <= 0 {
				ctx.SendChain(message.Text("图片发送失败"))
				continue
			}
			sent := model.SentImage{
				GroupID: ctx.Event.GroupID,
				PID:     illust.PID,
			}

			if err = service.DB.Save(&sent).Error; err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
			}
		}
	})

	engine.OnRegex(`^每日[色|涩|瑟]图$`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		/*		if !service.Acquire(ctx.Event.UserID) {
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("上一个任务还没结束，请稍后再试"))
					return
				}
				defer service.Release(ctx.Event.UserID)*/

		illusts, err := service.API.FetchPixivRecommend(1)
		if err != nil {
			ctx.SendChain(message.Text("发送涩图失败惹"))
			return
		}
		illust := illusts[0]
		img, usedFallback, err := service.API.Client.FetchPixivImage(illust, illust.OriginalURL)
		if err != nil {
			var httpErr *api.HTTPStatusError
			if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				_ = service.DB.DeleteIllustByPID(illust.PID)
			}
			ctx.SendChain(message.Text("发送涩图失败惹"))
			if usedFallback {
				service.triggerAutoSwitch()
			}
			return
		}
		if usedFallback {
			service.triggerAutoSwitch()
		}
		ctx.SendChain(message.Text(
			"PID:", illust.PID,
			"\n标题:", illust.Title,
			"\n画师:", illust.AuthorName,
			"\ntag:", illust.Tags,
			"\n收藏数:", illust.Bookmarks,
			"\n预览数:", illust.TotalView,
			"\n发布时间:", illust.CreateDate,
		), message.ImageBytes(img))
	})

	engine.OnRegex(`^(\d+)?张?[色|瑟|涩]图\s*(.+)?`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		limit := ctx.State["regex_matched"].([]string)[1]
		keyword := ctx.State["regex_matched"].([]string)[2]

		if !service.Acquire(ctx.Event.UserID) {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("上一个任务还没结束，请稍后再试"))
			return
		}
		defer service.Release(ctx.Event.UserID)

		if limit == "" {
			limit = "1"
		}

		if keyword == "" {
			keyword = defaultKeyword[rand.Intn(len(defaultKeyword))]
		}

		limitInt, err := strconv.Atoi(limit)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}

		if limitInt > 10 {
			ctx.SendChain(message.Text("图片太多了"))
			return
		}

		r18Req := api.IsR18(keyword)
		cleanKeyword := api.RemoveR18Keywords(keyword)

		gid := ctx.Event.GroupID
		if gid == 0 {
			gid = -ctx.Event.UserID
		}

		// 数据库中的 keyword 在缓存时已经去除了 R-18 关键词，因此查询时使用去除后的关键词
		cachedIllusts, err := service.DB.FindIllustsSmart(gid, cleanKeyword, limitInt, r18Req)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}

		cached, _ := service.DB.GetSentPictureIDs(gid)

		// 准备要发的图也要做过滤
		for _, ill := range cachedIllusts {
			cached = append(cached, ill.PID)
		}

		illusts, err := service.API.GetIllustsByKeyword(keyword, limitInt, cachedIllusts, cached)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}

		for _, illust := range illusts {
			fmt.Println(illust.PID)
			_ = service.DB.Create(&illust).Error
		}

		service.SendIllusts(ctx, illusts, gid)

		service.BackgroundCacheFiller(cleanKeyword, 10, r18Req, 5, ctx.Event.GroupID)
	})
}
