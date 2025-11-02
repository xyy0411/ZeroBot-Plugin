package pixiv

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/FloatTech/floatbox/file"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/jinzhu/gorm"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"io"
	"math/rand"
	"net/http"
	// _ "net/http/pprof"
	"net/url"
	"os"
	"strconv"
	"time"
)

var defaultKeyword = []string{"萝莉", "御姐", "妹妹", "姐姐"}

var defaultClient *http.Client

func init() {
	proxyURL, err := url.Parse("http://127.0.0.1:10809")
	if err != nil {
		log.Warning("连接代理错误:", err)
	}

	defaultClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MaxVersion: tls.VersionTLS13},
			Proxy:           http.ProxyURL(proxyURL),
		},
		Timeout: time.Minute,
	}
}

func init() {
	if file.IsNotExist("data/pixiv") {
		err := os.MkdirAll("data/pixiv", 0775)
		if err != nil {
			panic(err)
		}
	}
	var err error
	db, err = gorm.Open("sqlite3", "data/pixiv/pixiv.db")
	if err != nil {
		panic(err)
	}
	if err = db.AutoMigrate(&IllustCache{}, &SentImage{}, &RefreshToken{}).Error; err != nil {
		panic(err)
	}
	sqlDB := db.DB()
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	tokenResp = NewTokenStore()
	/*	go func() {
		// 使用 6060 端口
		log.Println("Starting pprof server on http://localhost:6060")
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			log.Printf("pprof server failed: %v", err)
		}
	}()*/
}

func init() {
	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "Pixiv 图片搜索",
		Help:             "- [x张]涩图 [关键词]\n- 每日涩图\n- [x张]画师[画师的uid] \n- p站搜图[插画pid] \n[]为可忽略项\n可添加多个关键词每个关键词用空格隔开\n默认不发R-18如果要发就加一个R-18关键词",
	}).ApplySingle(ctxext.NewGroupSingle("别着急，都会有的"))

	engine.OnRegex(`^下载代理*(.+)`, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		url := ctx.State["regex_matched"].([]string)[1]
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "v2rayN/5.38")
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			ctx.SendChain(message.Text("ERROR: ", resp.Status))
			return
		}
		rawData, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		nodes, err := ParseSubscription(string(rawData))
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		nodesBytes, err := json.Marshal(nodes)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		openFile, err := os.OpenFile(nodesFile, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		defer openFile.Close()
		_, err = openFile.Write(nodesBytes)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text("代理节点已更新"))
	})

	engine.OnFullMatch("切换代理节点", zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		autoSwitchConcurrent()
	})
	engine.OnRegex(`^设置p站token (.*)`, zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		token := ctx.State["regex_matched"].([]string)[1]
		var refreshToken RefreshToken
		refreshToken.User = ctx.Event.UserID
		refreshToken.Token = token
		if err := db.Save(&refreshToken).Error; err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}

		ctx.SendChain(message.Text("Pixiv Token: ", token))
	})

	engine.OnRegex(`^p站搜图(\d+)`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		rawPID := ctx.State["regex_matched"].([]string)[1]
		pid, err := strconv.ParseInt(rawPID, 10, 64)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		illust, err := FetchPixivByPID(pid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		img, err1 := illust.FetchPixivImage(true)
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
		illustInfos, err := FetchPixivByUser(uid, ctx.Event.GroupID, limitInt)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		for _, illust := range illustInfos {
			img, err1 := illust.FetchPixivImage()
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
			), message.ImageBytes(img)); msgID.ID() == 0 {
				ctx.SendChain(message.Text("图片发送失败"))
				continue
			}
			sent := SentImage{
				GroupID: ctx.Event.GroupID,
				PID:     illust.PID,
			}

			if err = db.Save(&sent).Error; err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
			}
		}
	})

	engine.OnRegex(`^每日[色|涩|瑟]图$`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		illusts, err := FetchPixivRecommend(1)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		img, err := illusts[0].FetchPixivImage()
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		illust := illusts[0]
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

		r18Req := isR18(keyword)                   // 是否要求 R-18
		cleanKeyword := removeR18Keywords(keyword) // 去掉 R-18 关键词

		illusts, err := GetIllustsByKeyword(cleanKeyword, r18Req, limitInt, ctx.Event.GroupID)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}

		for _, illust := range illusts {
			img, err1 := illust.FetchPixivImage()
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
			), message.ImageBytes(img)); msgID.ID() == 0 {
				continue
			}
			sent := SentImage{
				GroupID: ctx.Event.GroupID,
				PID:     illust.PID,
			}

			if err = db.Save(&sent).Error; err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
			}
		}

		BackgroundCacheFiller(cleanKeyword, 15, r18Req, 5, ctx.Event.GroupID)
	})
}
