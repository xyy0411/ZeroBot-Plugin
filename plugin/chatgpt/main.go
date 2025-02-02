// Package chatgpt 简易ChatGPT api聊天
package chatgpt

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/FloatTech/ttl"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type sessionKey struct {
	group int64
	user  int64
}

var (
	cache  = ttl.NewCache[sessionKey, []chatMessage](time.Minute * 15)
	engine = control.Register("chatgpt", &ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "chatgpt",
		Help: "-(chatgpt||//) [对话内容]\n" +
			"- 添加预设xxx xxx\n" +
			"- 设置(默认)预设xxx\n" +
			"- 设置gpt模型xxx\n" +
			"- 查看当前gpt模型" +
			"- 删除本群预设\n" +
			"- 查看预设列表\n" +
			"- (私聊发送)设置OpenAI apikey [apikey]\n" +
			"- (私聊发送)删除apikey\n" +
			"- (群聊发送)(授权|取消)(本群|全局)使用apikey\n" +
			"注:先私聊设置自己的key,再授权群聊使用,不会泄露key的\n",
		PrivateDataFolder: "chatgpt",
	})
)

func init() {
	engine.OnFullMatch(`^查看gpt url`, zero.OnlyPrivate, getdb).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		msg, err := db.findurl()
		if err != nil {
			ctx.SendChain(message.Text("查看ChatGPT url失败"))
			return
		}
		ctx.SendChain(message.Text("当前ChatGPT URL为:", msg))
	})
	engine.OnRegex(`^设置gpt url ([\s\S]*)`, zero.OnlyPrivate, getdb).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		err := db.seturl(1, ctx.State["regex_matched"].([]string)[1])
		if err != nil {
			ctx.SendChain(message.Text("设置ChatGPT url失败"))
			return
		}
		ctx.SendChain(message.Text("设置ChatGPT url成功"))
	})
	engine.OnRegex(`^设置gpt模型([\s\S]*)$`, zero.OnlyPrivate, getdb).SetBlock(false).
		Handle(func(ctx *zero.Ctx) {
			if zero.SuperUserPermission(ctx) {
			} else {
				return
			}
			err := db.insertmodel(-1, ctx.State["regex_matched"].([]string)[1])
			if err != nil {
				ctx.SendChain(message.Text("设置ChatGPT模型失败"))
				return
			}
			ctx.SendChain(message.Text("设置ChatGPT模型成功,当前模型为:", ctx.State["regex_matched"].([]string)[1]))
		})
	engine.OnRegex(`^查看当前gpt模型$`, getdb).SetBlock(false).
		Handle(func(ctx *zero.Ctx) {
			c, err := db.findmodel(-1)
			if err != nil {
				ctx.SendChain(message.Text("查看ChatGPT模型失败,[ERROR]:", err))
				return
			}
			ctx.SendChain(message.Text("当前ChatGPT模型为:", c))
		})

	engine.OnRegex(`^(?:chatgpt|//)([\s\S]*)$`, getdb).SetBlock(false).
		Handle(func(ctx *zero.Ctx) {
			var messages []chatMessage
			args := ctx.State["regex_matched"].([]string)[1]
			key := sessionKey{
				group: ctx.Event.GroupID,
				user:  ctx.Event.UserID,
			}
			if args == "reset" || args == "重置记忆" {
				cache.Delete(key)
				ctx.SendChain(message.Text("已清除上下文！"))
				return
			}
			apiKey, err := getkey(ctx)
			if err != nil {
				ctx.SendChain(message.Text("ERROR：", err))
				return
			}
			gid := ctx.Event.GroupID
			if gid == 0 {
				gid = -ctx.Event.UserID
			}
			// 添加预设
			content, err := db.findgroupmode(gid)
			if err == nil {
				messages = append(messages, chatMessage{
					Role:    "system",
					Content: content,
				})
				if len(cache.Get(key)) > 1 {
					messages = append(messages, cache.Get(key)[1:]...)
				}
			} else {
				c, err := db.findgroupmode(-1)
				if err != nil {
					messages = append(messages, cache.Get(key)...)
				} else {
					messages = append(messages, chatMessage{
						Role:    "system",
						Content: c,
					})
					if len(cache.Get(key)) > 1 {
						messages = append(messages, cache.Get(key)[1:]...)
					}
				}
			}
			mo, err := db.findmodel(-1)
			if err != nil {
				ctx.SendChain(message.Text("ERROR：", err))
				return
			}
			messages = append(messages, chatMessage{
				Role:    "user",
				Content: args,
			})
			resp, err := completions(messages, apiKey, mo)
			if err != nil {
				ctx.SendChain(message.Text("请求ChatGPT失败: ", err))
				return
			}
			reply := resp.Choices[0].Message
			reply.Content = strings.TrimSpace(reply.Content)
			re := regexp.MustCompile(`https://p16-flow-sign-va.ciciai.com/[^\s!]+`).FindString(reply.Content)
			messages = append(messages, reply)
			cache.Set(key, messages)
			if re != "" {
				response, err := http.Get(re)
				defer response.Body.Close()
				if err != nil {
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("ERROR :", err))
					return
				}
				bytes, err := io.ReadAll(response.Body)
				if err != nil {
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("ERROR :", err))
					return
				}
				ctx.SendChain(message.Reply(ctx.Event.MessageID), message.ImageBytes(bytes), message.Text("\n本次消耗token: ", resp.Usage.PromptTokens, "+", resp.Usage.CompletionTokens, "=", resp.Usage.TotalTokens))
			} else {
				ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(reply.Content),
					message.Text("\n本次消耗token: ", resp.Usage.PromptTokens, "+", resp.Usage.CompletionTokens, "=", resp.Usage.TotalTokens))
			}
		})
	engine.OnRegex(`^设置\s*OpenAI\s*apikey\s*([\s\S]*)$`, zero.OnlyPrivate, getdb).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		err := db.insertkey(-ctx.Event.UserID, ctx.State["regex_matched"].([]string)[1])
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		ctx.SendChain(message.Text("保存apikey成功"))
	})
	engine.OnFullMatch("删除apikey", getdb).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			err := db.delkey(-ctx.Event.UserID)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text("保存apikey成功"))
		})
	engine.OnRegex(`^添加预设\s*(\S+)\s+(.*)$`, zero.SuperUserPermission, getdb).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			modename := ctx.State["regex_matched"].([]string)[1]
			content := ctx.State["regex_matched"].([]string)[2]
			err := db.insertmode(modename, content)
			if err != nil {
				ctx.SendChain(message.Text("添加失败: ", err))
				return
			}
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("添加成功"))
		})
	engine.OnRegex(`^设置(默认)?预设\s*(\S+)$`, getdb).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			codename := ctx.State["regex_matched"].([]string)[2]
			gid := ctx.Event.GroupID
			if ctx.State["regex_matched"].([]string)[1] == "" {
				if gid == 0 {
					gid = -ctx.Event.UserID
				}
			} else {
				gid = -1 // 全局为-1的群号
			}
			err := db.changemode(gid, codename)
			if err != nil {
				ctx.SendChain(message.Text("设置失败: ", err))
				return
			}
			ctx.SendChain(message.Text("设置成功"))
			for _, v := range ctx.GetThisGroupMemberListNoCache().Array() {
				cache.Delete(
					sessionKey{
						group: ctx.Event.GroupID,
						user:  v.Get("user_id").Int(),
					})
			}
			ctx.SendChain(message.Text("本群记忆清除成功"))
		})
	engine.OnFullMatch("删除本群预设", getdb).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid := ctx.Event.GroupID
			if gid == 0 {
				gid = -ctx.Event.UserID
			}
			err := db.delgroupmode(gid)
			if err != nil {
				ctx.SendChain(message.Text("删除失败: ", err))
				return
			}
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("删除成功"))
			for _, v := range ctx.GetThisGroupMemberListNoCache().Array() {
				cache.Delete(
					sessionKey{
						group: ctx.Event.GroupID,
						user:  v.Get("user_id").Int(),
					})
			}
			ctx.SendChain(message.Text("本群记忆清除成功"))
		})
	engine.OnRegex(`^查看预设\s*(\S+)$`, getdb).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			if ctx.State["regex_matched"].([]string)[1] == "列表" {
				pre, err := db.findformode()
				if err != nil {
					ctx.SendChain(message.Text(message.Reply(ctx.Event.MessageID), "当前没有任何预设: ", err))
					return
				}
				ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(pre))
				return
			}
			if zero.AdminPermission(ctx) {
				content, err := db.findmode(ctx.State["regex_matched"].([]string)[1])
				if err != nil {
					ctx.SendChain(message.Text("ERROR: ", err))
					return
				}
				ctx.SendChain(message.Text(content))
			}
		})
	engine.OnRegex(`^(取消|授权)(全局|本群)使用apikey$`, getdb).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			if ctx.State["regex_matched"].([]string)[2] == "全局" {
				if !zero.SuperUserPermission(ctx) {
					ctx.SendChain(message.Text("失败: 权限不足"))
					return
				}
				if ctx.State["regex_matched"].([]string)[1] == "授权" {
					err := db.insertgkey(-ctx.Event.UserID, -1)
					if err != nil {
						ctx.SendChain(message.Text("授权失败: ", err))
						return
					}
					ctx.SendChain(message.Text("授权成功"))
					return
				}
				err := db.delgkey(-1)
				if err != nil {
					ctx.SendChain(message.Text("取消失败: ", err))
					return
				}
				ctx.SendChain(message.Text("取消成功: ", err))
				return
			}
			if ctx.State["regex_matched"].([]string)[1] == "授权" {
				err := db.insertgkey(-ctx.Event.UserID, ctx.Event.GroupID)
				if err != nil {
					ctx.SendChain(message.Text("授权失败: ", err))
					return
				}
				ctx.SendChain(message.Text("授权成功"))
				return
			}
			t, err := db.findgtoqq(ctx.Event.GroupID)
			if err != nil {
				ctx.SendChain(message.Text("取消失败: ", err))
				return
			}
			if t != -ctx.Event.UserID {
				ctx.SendChain(message.Text("取消失败: 你不是授权用户"))
				return
			}
			err = db.delgkey(ctx.Event.GroupID)
			if err != nil {
				ctx.SendChain(message.Text("取消失败: ", err))
				return
			}
			ctx.SendChain(message.Text("取消成功"))
		})
}
