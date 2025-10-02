package pixiv

import (
	"crypto/tls"
	"fmt"
	"github.com/FloatTech/floatbox/file"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/jinzhu/gorm"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
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
		log.Print("连接代理错误:", err)
	}

	defaultClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MaxVersion: tls.VersionTLS13},
			Proxy:           http.ProxyURL(proxyURL),
		},
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
	sqlDB.SetMaxOpenConns(10)           // 最多 10 个连接
	sqlDB.SetMaxIdleConns(5)            // 最多保留 5 个空闲连接
	sqlDB.SetConnMaxLifetime(time.Hour) // 一个连接最多用 1 小时

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
		Help:             "-[x张]色图 [关键词]\n []为可忽略项\n可添加多个关键词每个关键词用空格隔开\n默认不发R-18如果要发就加一个R-18关键词",
	})

	engine.OnRegex(`^设置p站token (.*)`, zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		ctx.SendChain(message.Text("1"))
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

	engine.OnRegex(`^(\d+)?张?色图\s*(.+)?`).SetBlock(true).Handle(func(ctx *zero.Ctx) {
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

		r18Req := isR18(keyword)                   // 是否用户要求 R-18
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
			ctx.SendChain(message.Text(
				"PID:", illust.PID,
				"\n标题:", illust.Title,
				"\n画师:", illust.AuthorName,
				"\n收藏数:", illust.Bookmarks,
				"\n预览数:", illust.TotalView,
				"\n发布时间:", illust.CreateDate,
			), message.Image(img))
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
