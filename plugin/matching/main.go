package matching

import (
	"errors"
	"fmt"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/gorilla/websocket"
	"github.com/jinzhu/gorm"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	helpFilePath = "./plugin/matching/help_info.txt"
	engine       = control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault:  false,
		Brief:             "匹配",
		Help:              readHelpInfo(),
		PrivateDataFolder: "matching",
	})
)

// readHelpInfo 从文件中读取帮助信息
func readHelpInfo() string {
	content, err := os.ReadFile(helpFilePath)
	if err != nil {
		fmt.Printf("读取帮助信息文件失败: %v\n", err)
		return ""
	}
	return string(content)
}

var regexpstring = `^(有无|有人|谁来)(联机|匹配|打架|对决|玩吗|to|qd|lh|uu|主机|副机|主副皆可|主|副|联机吗)+`

func init() {
	engine.OnFullMatch("退出被动匹配黑名单", getDB, zero.OnlyPrivate).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		err := db.Where("user_id=?", uid).Delete(&RejectedMatchUser{}).Error
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		ctx.SendChain(message.Text("已退出被动匹配黑名单"))
	})
	engine.OnRegex(regexpstring, getDB).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		gid := ctx.Event.GroupID

		if gid == 0 {
			gid = -1
		}
		err := db.Where("user_id= ?", uid).First(&RejectedMatchUser{}).Error
		if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		var user User

		resp, err := http.Get("http://127.0.0.1:3000/api/matching/status/" + strconv.FormatInt(uid, 10))
		if err != nil || resp.StatusCode == http.StatusOK {
			return
		}

		err = db.Preload("OnlineSoftware").Preload("BlUser").Where("user_id = ?", uid).First(&user).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		if len(user.OnlineSoftware) == 0 {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("发现有用户联机，当前未设置匹配软件,现在帮你使用to主副皆可进行默认设置是否开始匹配?[输入:(是|否|td|退订)]"))
		} else {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("发现有用户联机,是否使用已经设置完成的软件开始匹配?[是|否]"))
		}
		recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.RegexRule(`(是|否|td|退订)$`)).Repeat()
		defer cancel()

		for {
			select {
			case <-time.After(time.Minute * 2):
				return
			case r := <-recv:
				if r.Event.Message.String() != "是" {
					if err := db.Save(&RejectedMatchUser{UserID: uid}).Error; err != nil {
						ctx.SendChain(message.Text("ERROR:", err))
						return
					}
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("好哦，以后就不打扰你啦"))
					return
				}
				if user.UserID == 0 {
					user = User{
						OnlineSoftware: []*OnlineSoftware{
							{Name: "to", Type: 0},
						},
						GroupID:   gid,
						UserID:    uid,
						LimitTime: 900,
					}
				}
				processMatching(ctx, user)
			}
		}
	})
	engine.OnFullMatch("查看我的匹配状态", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			req, err := http.Get("http://127.0.0.1:3000/api/matching/status/" + strconv.FormatInt(uid, 10))
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			defer req.Body.Close()
			body, err := io.ReadAll(req.Body)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text(string(body)))
		})

	engine.OnFullMatch("查看匹配时间", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置匹配时间!\nERROR:", err))
				return
			}
			ctx.SendChain(message.Text("匹配时间为", user.LimitTime/60, "分钟"))
		})
	engine.OnRegex(`删除匹配软件 (.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			softwareName := ctx.State["regex_matched"].([]string)[1]
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置匹配软件!\nERROR:", err))
				return
			}

			if err := db.Where("matching_id =?", user.ID).Where("name = ?", softwareName).Delete(&OnlineSoftware{}).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置这个匹配软件"))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("%s[%d]删除匹配软件成功", ctx.CardOrNickName(uid), uid)))
		})
	engine.OnRegex(`删除匹配黑名单 (.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			blockUser := ctx.State["regex_matched"].([]string)[1]
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置黑名单!\nERROR:", err))
				return
			}

			if err := db.Where("user_id = ?", user.ID).Where("block_user_id =?", blockUser).Delete(&blockUser).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置这个黑名单"))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("%s[%d]删除黑名单成功", ctx.CardOrNickName(uid), uid)))
		})
	engine.OnFullMatch("查看匹配黑名单列表", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			var msg strings.Builder
			var blockUsers []*BlockUser
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置黑名单!\nERROR:", err))
				return
			}
			if err := db.Where("matching_id =?", user.ID).Find(&blockUsers).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置黑名单!\nERROR:", err))
				return
			}

			msg.WriteString("黑名单列表:\n")
			for i, blockUser := range blockUsers {
				msg.WriteString("第")
				msg.WriteString(strconv.Itoa(i + 1))
				msg.WriteString("个 ")
				msg.WriteString(strconv.FormatInt(blockUser.BlUser, 10) + "\n")
			}
			ctx.SendChain(message.Text(msg.String()))
		})
	engine.OnFullMatch("查看我的匹配软件", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置匹配软件!\nERROR:", err))
				return
			}

			var SoftwareList []*OnlineSoftware
			if err := db.Where("matching_id =?", user.ID).Find(&SoftwareList).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置匹配软件!\nERROR:", err))
				return
			}

			var msg strings.Builder
			msg.WriteString("当前可用匹配软件如下:\n")

			for i, software := range SoftwareList {
				// 判断是否为最后一个元素
				suffix := ": "
				if i == len(SoftwareList)-1 {
					suffix = ""
				}
				var softwareType string
				switch software.Type {
				case 0:
					softwareType = "主副皆可"
				case 1:
					softwareType = "仅主"
				case 2:
					softwareType = "仅副"
				}
				msg.WriteString("第")
				msg.WriteString(strconv.Itoa(i + 1))
				msg.WriteString("个 ")
				msg.WriteString(software.Name + suffix + softwareType + "\n")
			}

			ctx.SendChain(message.Text(msg.String()))
		})
	engine.OnRegex(`设置匹配黑名单 (.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			blockUser := ctx.State["regex_matched"].([]string)[1]
			blockUserInt, err := strconv.ParseInt(blockUser, 10, 64)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
				user = User{
					UserID:   uid,
					UserName: ctx.CardOrNickName(uid),
				}
				if err := db.Save(&user).Error; err != nil {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
			}

			b := BlockUser{
				MatchingID: user.ID,
				BlUser:     blockUserInt,
			}

			if err := db.Save(&b).Error; err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}

			ctx.SendChain(message.Text(fmt.Sprintf("%s[%d]添加黑名单成功", ctx.CardOrNickName(uid), uid)))
		})
	engine.OnRegex(`设置匹配时间 (.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			limitTime := ctx.State["regex_matched"].([]string)[1]
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
				user = User{
					UserID:   uid,
					UserName: ctx.CardOrNickName(uid),
				}
			}
			t, err := strconv.Atoi(limitTime)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			user.LimitTime = int64(t * 60)

			if err := db.Save(&user).Error; err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text(fmt.Sprintf("%s[%d]设置匹配时间成功", ctx.CardOrNickName(uid), uid)))

		})
	engine.OnRegex(`设置匹配软件 (.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			software := ctx.State["regex_matched"].([]string)[1]
			// 去空格
			software = strings.ReplaceAll(software, " ", "")
			// 全部换成小写
			software = strings.ToLower(software)
			var user User
			if err := db.Where("user_id =?", uid).First(&user).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
				user = User{
					UserID:   uid,
					UserName: ctx.CardOrNickName(uid),
				}
				if err := db.Create(&user).Error; err != nil {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
			}
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("请输入软件类型[主副皆可|仅主|仅副]"))
			recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.RegexRule(`^(.+)$`)).Repeat()
			defer cancel()
			timer := time.NewTimer(120 * time.Second)
			answer := ""
			defer timer.Stop()
			for {
				select {
				case <-timer.C:
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("超时未输入"))
					return
				case r := <-recv:
					answer = r.Event.Message.String()

					var softwareType int8
					switch answer {
					case "仅主":
						softwareType = 1
					case "仅副":
						softwareType = 2
					case "主副皆可":
						softwareType = 0
					default:
						ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("输入错误"))
						return
					}
					tx := db.Begin()

					updateResult := tx.Model(&OnlineSoftware{}).
						Where("matching_id = ? AND name = ?", user.ID, software).
						Update("type", softwareType)

					if updateResult.Error == nil && updateResult.RowsAffected > 0 {
						tx.Commit()
						ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(fmt.Sprintf("%s[%d]更新软件匹配类型成功", ctx.CardOrNickName(uid), uid)))
						return
					}

					s := OnlineSoftware{
						MatchingID: user.ID,
						Name:       software,
						Type:       softwareType,
					}
					if err := tx.Create(&s).Error; err != nil {
						tx.Rollback()
						ctx.SendChain(message.Reply(ctx.Event.MessageID), message.At(uid), message.Text(fmt.Sprintf("%s[%d]添加软件匹配类型失败\nERROR:%v", ctx.CardOrNickName(uid), uid, err)))
						return
					}
					tx.Commit()
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(fmt.Sprintf("%s[%d]添加软件匹配类型成功", ctx.CardOrNickName(uid), uid)))
					return
				}
			}
		})
	engine.OnFullMatchGroup([]string{"取消匹配", "退出匹配"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			req, err := http.NewRequest("DELETE", "http://127.0.0.1:3000/api/matching/"+strconv.FormatInt(uid, 10), nil)
			if err != nil {
				ctx.SendChain(message.Text(err))
				return
			}
			client := &http.Client{}
			res, err := client.Do(req)
			if err != nil {
				ctx.SendChain(message.Text(err))
				return
			}
			defer res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(string(body)))
		})
	engine.OnFullMatchGroup([]string{"开始匹配", "匹配", "匹配开始"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid := ctx.Event.GroupID
			uid := ctx.Event.UserID
			var user User
			if err := db.Where("user_id = ?", uid).First(&user).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
				ctx.SendChain(message.Text("你还没有设置匹配软件呢"))
				return
			}
			if gid == 0 {
				user.GroupID = -1
			} else {
				user.GroupID = gid
			}

			var software []*OnlineSoftware
			if err := db.Where("matching_id =?", user.ID).Find(&software).Error; err != nil {
				ctx.SendChain(message.Text("你还没有设置匹配软件ERROR:", err))
				return
			}
			if len(software) == 0 {
				ctx.SendChain(message.Text("你还没有设置匹配软件"))
				return
			}

			var blockUsers []*BlockUser
			db.Where("user_id =?", user.ID).Find(&blockUsers)

			user.BlUser = blockUsers
			user.OnlineSoftware = software
			processMatching(ctx, user)
		})
}

func processMatching(ctx *zero.Ctx, user User) {
	var dl websocket.Dialer
	conn, _, err := dl.Dial(fmt.Sprintf("ws://127.0.0.1:3000/api/matching/%d", user.UserID), nil)
	if err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}

	err = conn.WriteJSON(user)
	if err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}
	defer conn.Close()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) || errors.Is(err, io.EOF) {
				return
			}
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.At(ctx.Event.UserID), message.Text(string(msg)))
	}
}
