package pixiv

import (
	"errors"
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/api"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/cache"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"github.com/FloatTech/floatbox/file"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
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

	pixivAPI := api.NewPixivAPI(t1.Token)
	service = NewService(db, pixivAPI)
}

func init() {

	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "Pixiv 图片搜索",
		Help:             "- [x张]涩图 [关键词]\n- 每日涩图\n- [x张]画师[画师的uid] \n- p站搜图[插画pid]\n[]为可忽略项\n可添加多个关键词每个关键词用空格隔开\n默认不发R-18如果要发就加一个R-18关键词",
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
		img, err1 := service.API.Client.FetchPixivImage(*illust, illust.OriginalURL)
		if err1 != nil {
			var httpErr *api.HTTPStatusError
			if errors.As(err1, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				_ = service.DB.DeleteIllustByPID(illust.PID)
			}
			ctx.SendChain(message.Text("ERROR: ", err1))
			return
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

		service.SendIllusts(ctx, illustInfos)
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
		img, err := service.API.Client.FetchPixivImage(illust, illust.OriginalURL)
		if err != nil {
			var httpErr *api.HTTPStatusError
			if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				_ = service.DB.DeleteIllustByPID(illust.PID)
			}
			ctx.SendChain(message.Text("发送涩图失败惹"))
			return
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

		service.SendIllusts(ctx, illusts)

		service.BackgroundCacheFiller(cleanKeyword, 10, r18Req, 5, ctx.Event.GroupID)
	})
}
