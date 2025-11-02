package pixiv

import (
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/api"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/cache"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/proxy"
	"github.com/FloatTech/floatbox/file"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"math/rand"

	"os"
	"strconv"
)

var defaultKeyword = []string{"萝莉", "御姐", "妹妹", "姐姐"}

var service *Service

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
	manager := proxy.NewManager()
	service = NewService(db, pixivAPI, manager)
}

func init() {

	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "Pixiv 图片搜索",
		Help:             "- [x张]涩图 [关键词]\n- 每日涩图\n- [x张]画师[画师的uid] \n- p站搜图[插画pid] \n[]为可忽略项\n可添加多个关键词每个关键词用空格隔开\n默认不发R-18如果要发就加一个R-18关键词",
	}).ApplySingle(ctxext.NewGroupSingle("别着急，都会有的"))

	engine.OnRegex(`^下载代理*(.+)`, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		url := ctx.State["regex_matched"].([]string)[1]
		err := service.Proxy.DownloadingNode(url)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
		}
		ctx.SendChain(message.Text("代理节点已更新"))
	})

	engine.OnFullMatch("切换代理节点", zero.SuperUserPermission).SetBlock(false).Handle(func(ctx *zero.Ctx) {
		service.Proxy.AutoSwitch()
		ctx.SendChain(message.Text("切换代理节点完成"))
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
		img, err1 := service.API.Client.FetchPixivImage(*illust, illust.OriginalURL, true)
		if err1 != nil {
			ctx.SendChain(message.Text("ERROR: ", err1))
			return
		}
		fmt.Println("获取", illust.PID, "成功，准备发送！", float64(len(img))/1024/1024, "mb")
		ctx.SendChain(message.Text(
			"PID:", illust.PID,
			"\n标题:", illust.Title,
			"\n画师:", illust.AuthorName,
			"\n收藏数:", illust.Bookmarks,
			"\n预览数:", illust.TotalView,
			"\n发布时间:", illust.CreateDate,
		), message.ImageBytes(img))
	})

	engine.OnRegex(`^(\d+)?张?画师(\d+)`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
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

			img, err1 := service.API.Client.FetchPixivImage(illust, illust.OriginalURL)
			if err1 != nil {
				ctx.SendChain(message.Text("ERROR: ", err1))
				continue
			}
			fmt.Println("获取", illust.PID, "成功，准备发送！", float64(len(img))/1024/1024, "mb")
			if msgID := ctx.SendChain(message.Text(
				"PID:", illust.PID,
				"\n标题:", illust.Title,
				"\n画师:", illust.AuthorName,
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
		illusts, err := service.API.FetchPixivRecommend(1)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		illust := illusts[0]
		img, err := service.API.Client.FetchPixivImage(illust, illust.OriginalURL, true)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text(
			"PID:", illust.PID,
			"\n标题:", illust.Title,
			"\n画师:", illust.AuthorName,
			"\n收藏数:", illust.Bookmarks,
			"\n预览数:", illust.TotalView,
			"\n发布时间:", illust.CreateDate,
		), message.ImageBytes(img))
	})

	engine.OnRegex(`^(\d+)?张?[色|瑟|涩]图\s*(.+)?`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		limit := ctx.State["regex_matched"].([]string)[1]
		keyword := ctx.State["regex_matched"].([]string)[2]

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

		cachedIllusts, err := service.DB.FindIllustsSmart(ctx.Event.GroupID, cleanKeyword, limitInt, r18Req)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}

		cached := service.DB.FindCached(cleanKeyword)

		illusts, err := service.API.GetIllustsByKeyword(keyword, limitInt, cachedIllusts, cached)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}

		for _, illust := range illusts {

			_ = service.DB.Create(illust).Error

			img, err1 := service.API.Client.FetchPixivImage(illust, illust.OriginalURL)
			if err1 != nil {
				ctx.SendChain(message.Text("ERROR: ", err1))
				continue
			}
			fmt.Println("获取", illust.PID, "成功，准备发送！", float64(len(img))/1024/1024, "mb")
			if msgID := ctx.SendChain(message.Text(
				"PID:", illust.PID,
				"\n标题:", illust.Title,
				"\n画师:", illust.AuthorName,
				"\n收藏数:", illust.Bookmarks,
				"\n预览数:", illust.TotalView,
				"\n发布时间:", illust.CreateDate,
			), message.ImageBytes(img)); msgID.ID() <= 0 {
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

		service.BackgroundCacheFiller(cleanKeyword, 15, r18Req, 5, ctx.Event.GroupID)
	})
}
