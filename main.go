// Package main ZeroBot-Plugin main file
package main

//go:generate go run github.com/FloatTech/ZeroBot-Plugin/abineundo/ref -r .

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/FloatTech/ZeroBot-Plugin/abineundo" // 设置插件优先级&更改控制台属性
	"github.com/FloatTech/ZeroBot-Plugin/kanban"      // 打印 banner

	// ---------以下插件均可通过前面加 // 注释，注释后停用并不加载插件--------- //
	// ----------------------插件优先级按顺序从高到低---------------------- //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	// ----------------------------高优先级区---------------------------- //
	// vvvvvvvvvvvvvvvvvvvvvvvvvvvv高优先级区vvvvvvvvvvvvvvvvvvvvvvvvvvvv //
	//               vvvvvvvvvvvvvv高优先级区vvvvvvvvvvvvvv               //
	//                      vvvvvvv高优先级区vvvvvvv                      //
	//                          vvvvvvvvvvvvvv                          //
	//                               vvvv                               //

	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/antiabuse" // 违禁词

	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/chat" // 基础词库

	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/manager" // 群管

	_ "github.com/FloatTech/zbputils/job" // 定时指令触发器

	//                               ^^^^                               //
	//                          ^^^^^^^^^^^^^^                          //
	//                      ^^^^^^^高优先级区^^^^^^^                      //
	//               ^^^^^^^^^^^^^^高优先级区^^^^^^^^^^^^^^               //
	// ^^^^^^^^^^^^^^^^^^^^^^^^^^^^高优先级区^^^^^^^^^^^^^^^^^^^^^^^^^^^^ //
	// ----------------------------高优先级区---------------------------- //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	// ----------------------------中优先级区---------------------------- //
	// vvvvvvvvvvvvvvvvvvvvvvvvvvvv中优先级区vvvvvvvvvvvvvvvvvvvvvvvvvvvv //
	//               vvvvvvvvvvvvvv中优先级区vvvvvvvvvvvvvv               //
	//                      vvvvvvv中优先级区vvvvvvv                      //
	//                          vvvvvvvvvvvvvv                          //
	//                               vvvv                               //

	_ "github.com/FloatTech/ZeroBot-Plugin/custom"               // 自定义插件合集
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/aifalse"       // 服务器监控
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/aiimage"       // AI画图
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/animetrace"    // AnimeTrace 动画/Galgame识别
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/autowithdraw"  // 触发者撤回时也自动撤回
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/bilibili"      // b站相关
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/bilibiliparse" // b站相关
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/bilibilipush"  // b站相关
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/chess"         // 国际象棋
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/dailynews"     // 今日早报
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/danbooru"      // DeepDanbooru二次元图标签识别
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/emojimix"      // 合成emoji
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/event"         // 好友申请群聊邀请事件处理
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/fortune"       // 运势
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/gif"           // 制图
	// _ "github.com/FloatTech/ZeroBot-Plugin/plugin/matching"          // bvn匹配
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/mcfish"   // 钓鱼模拟器
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/niuniu"   // 牛牛大作战
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv"    // pixiv
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/qqwife"   // 一群一天一夫一妻制群老婆
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/qzone"    // qq空间表白墙
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/robbery"  // 打劫群友的ATRI币
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/runcode"  // 在线运行代码
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/saucenao" // 以图搜图
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/score"    // 分数
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/shindan"  // 测定
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/tarot"    // 抽塔罗牌
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/tracemoe" // 搜番
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/wallet"   // 钱包
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/wife"     // 抽老婆
	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/wordle"   // 猜单词

	//                               ^^^^                               //
	//                          ^^^^^^^^^^^^^^                          //
	//                      ^^^^^^^中优先级区^^^^^^^                      //
	//               ^^^^^^^^^^^^^^中优先级区^^^^^^^^^^^^^^               //
	// ^^^^^^^^^^^^^^^^^^^^^^^^^^^^中优先级区^^^^^^^^^^^^^^^^^^^^^^^^^^^^ //
	// ----------------------------中优先级区---------------------------- //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	// ----------------------------低优先级区---------------------------- //
	// vvvvvvvvvvvvvvvvvvvvvvvvvvvv低优先级区vvvvvvvvvvvvvvvvvvvvvvvvvvvv //
	//               vvvvvvvvvvvvvv低优先级区vvvvvvvvvvvvvv               //
	//                      vvvvvvv低优先级区vvvvvvv                      //
	//                          vvvvvvvvvvvvvv                          //
	//                               vvvv                               //

	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/aichatcfg" // AI聊天配置

	_ "github.com/FloatTech/ZeroBot-Plugin/plugin/aichat" // AI聊天

	//                               ^^^^                               //
	//                          ^^^^^^^^^^^^^^                          //
	//                      ^^^^^^^低优先级区^^^^^^^                      //
	//               ^^^^^^^^^^^^^^低优先级区^^^^^^^^^^^^^^               //
	// ^^^^^^^^^^^^^^^^^^^^^^^^^^^^低优先级区^^^^^^^^^^^^^^^^^^^^^^^^^^^^ //
	// ----------------------------低优先级区---------------------------- //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	//                                                                  //
	// -----------------------以下为内置依赖，勿动------------------------ //
	"github.com/FloatTech/floatbox/file"
	"github.com/FloatTech/floatbox/process"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/driver"
	"github.com/wdvxdr1123/ZeroBot/message"

	// webctrl "github.com/FloatTech/zbputils/control/web"

	"github.com/FloatTech/ZeroBot-Plugin/kanban/banner"
	// -----------------------以上为内置依赖，勿动------------------------ //
)

type zbpcfg struct {
	Z               zero.Config        `json:"zero"`
	W               []*driver.WSClient `json:"ws"`
	S               []*driver.WSServer `json:"wss"`
	ForceBase64File bool               `json:"force_base64_file"`
}

var config zbpcfg

func init() {
	sus := make([]int64, 0, 16)
	// 解析命令行参数
	d := flag.Bool("d", false, "Enable debug level log and higher.")
	w := flag.Bool("w", false, "Enable warning level log and higher.")
	h := flag.Bool("h", false, "Display this help.")
	// g := flag.String("g", "127.0.0.1:3000", "Set webui url.")
	// 直接写死 AccessToken 时，请更改下面第二个参数
	token := flag.String("t", "s_d3C_bWf-74bLzm", "Set AccessToken of WSClient.")
	// 直接写死 URL 时，请更改下面第二个参数
	url := flag.String("u", "ws://124.156.193.131:3002", "Set Url of WSClient.")
	// 默认昵称
	adana := flag.String("n", "亚托莉", "Set default nickname.")
	prefix := flag.String("p", "/", "Set command prefix.")
	runcfg := flag.String("c", "", "Run from config file.")
	save := flag.String("s", "", "Save default config to file and exit.")
	late := flag.Uint("l", 233, "Response latency (ms).")
	rsz := flag.Uint("r", 4096, "Receiving buffer ring size.")
	maxpt := flag.Uint("x", 4, "Max process time (min).")
	markmsg := flag.Bool("m", false, "Don't mark message as read automatically")
	fb64 := flag.Bool("fb64", false, "Force to send base64 file.")
	flag.BoolVar(&file.SkipOriginal, "mirror", false, "Use mirrored lazy data at first")

	flag.Parse()

	if *h {
		fmt.Println("Usage:")
		flag.PrintDefaults()
		os.Exit(0)
	}
	if *d && !*w {
		logrus.SetLevel(logrus.DebugLevel)
	}
	if *w {
		logrus.SetLevel(logrus.WarnLevel)
	}

	for _, s := range flag.Args() {
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			continue
		}
		sus = append(sus, i)
	}

	// 通过代码写死的方式添加主人账号
	sus = append(sus, 3061196825)
	// sus = append(sus, 87654321)

	// 启用 webui
	// go webctrl.RunGui(*g)

	if *runcfg != "" {
		f, err := os.Open(*runcfg)
		if err != nil {
			panic(err)
		}
		config.W = make([]*driver.WSClient, 0, 2)
		err = json.NewDecoder(f).Decode(&config)
		f.Close()
		if err != nil {
			panic(err)
		}
		config.Z.Driver = make([]zero.Driver, len(config.W)+len(config.S))
		for i, w := range config.W {
			config.Z.Driver[i] = w
		}
		for i, s := range config.S {
			config.Z.Driver[i+len(config.W)] = s
		}
		logrus.Infoln("[main] 从", *runcfg, "读取配置文件")
		return
	}
	config.W = []*driver.WSClient{driver.NewWebSocketClient(*url, *token)}
	config.Z = zero.Config{
		NickName:       append([]string{*adana}, "ATRI", "atri", "亚托莉", "アトリ"),
		CommandPrefix:  *prefix,
		SuperUsers:     sus,
		RingLen:        *rsz,
		Latency:        time.Duration(*late) * time.Millisecond,
		MaxProcessTime: time.Duration(*maxpt) * time.Minute,
		MarkMessage:    !*markmsg,
		Driver:         []zero.Driver{config.W[0]},
	}
	config.ForceBase64File = *fb64

	if *save != "" {
		f, err := os.Create(*save)
		if err != nil {
			panic(err)
		}
		err = json.NewEncoder(f).Encode(&config)
		f.Close()
		if err != nil {
			panic(err)
		}
		logrus.Infoln("[main] 配置文件已保存到", *save)
		os.Exit(0)
	}
}

func main() {
	if !strings.Contains(runtime.Version(), "go1.2") { // go1.20之前版本需要全局 seed，其他插件无需再 seed
		rand.Seed(time.Now().UnixNano()) //nolint: staticcheck
	}
	message.SetForceBase64File(config.ForceBase64File)
	// 帮助
	zero.OnFullMatchGroup([]string{"help", "/help", ".help", "菜单"}, zero.OnlyToMe).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			ctx.SendChain(message.Text(banner.Banner, "\n管理发送\"/服务列表\"查看 bot 功能\n发送\"/用法name\"查看功能用法"))
		})
	zero.OnFullMatch("查看zbp公告", zero.OnlyToMe, zero.AdminPermission).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			ctx.SendChain(message.Text(strings.ReplaceAll(kanban.Kanban(), "\t", "")))
		})
	zero.RunAndBlock(&config.Z, process.GlobalInitMutex.Unlock)
}
