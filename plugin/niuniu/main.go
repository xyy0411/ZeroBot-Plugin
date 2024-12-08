// Package niuniu 牛牛大作战
package niuniu

import (
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/niu"
	"strconv"
	"time"

	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/RomiChan/syncx"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/extension/rate"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type lastLength struct {
	TimeLimit int64
	Count     int
	Length    float64
}

type propsCount struct {
	Count     int
	TimeLimit time.Time
}

var (
	en = control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "牛牛大作战",
		Help: "- 打胶\n" +
			"- 使用[道具名称]打胶\n" +
			"- jj@xxx\n" +
			"- 使用[道具名称]jj@xxx\n" +
			"- 注册牛牛\n" +
			"- 赎牛牛(cd:60分钟)\n" +
			"- 出售牛牛\n" +
			"- 牛牛商店\n" +
			"- 牛牛背包\n" +
			"- 注销牛牛\n" +
			"- 查看我的牛牛\n" +
			"- 牛子长度排行\n" +
			"- 牛子深度排行\n",
		PrivateDataFolder: "niuniu",
	})
	dajiaoLimiter = rate.NewManager[string](time.Second*90, 1)
	jjLimiter     = rate.NewManager[string](time.Second*150, 1)
	jjCount       = syncx.Map[string, *lastLength]{}
)

func init() {
	/*	en.OnRegex(`^购买(/d+)个牛牛`, zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		n, _ := strconv.Atoi(ctx.State["regex_matched"].([]string)[1])

	})*/
	en.OnFullMatch("查看牛牛拍卖行", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		auction, err := niu.ShowAuction(gid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		var messages message.Message
		messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text("牛牛拍卖行有以下牛牛")))
		for _, info := range auction {
			msg := fmt.Sprintf("商品牛牛: %d\n牛牛源所属: %d\n牛牛价格: %d\n牛牛大小: %.2f",
				info.ID, info.UserId, info.Money, info.Length)
			messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text(msg)))
		}
		if id := ctx.Send(messages).ID(); id == 0 {
			ctx.Send(message.Text("发送拍卖行失败"))
			return
		}
	})
	en.OnFullMatch("出售牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		sell, err := niu.Sell(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(sell))
	})
	en.OnFullMatch("牛牛背包", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		bag, err := niu.Bag(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(bag))
	})
	en.OnFullMatch("牛牛商店", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID

		propMap := map[int]struct {
			name        string
			cost        int
			scope       string
			description string
			count       int
		}{
			1: {"伟哥", 300, "打胶", "可以让你打胶每次都增长", 5},
			2: {"媚药", 300, "打胶", "可以让你打胶每次都减少", 5},
			3: {"击剑神器", 500, "jj", "可以让你每次击剑都立于不败之地", 2},
			4: {"击剑神稽", 500, "jj", "可以让你每次击剑都失败", 2},
		}

		var messages message.Message
		messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text("牛牛商店当前售卖的物品如下")))
		for id, product := range propMap {
			productInfo := fmt.Sprintf("商品%d\n商品名: %s\n商品价格: %dATRI币\n商品作用域: %s商品描述: %s\n库存数量:%d",
				id, product.name, product.cost, product.scope, product.description, product.count)
			messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text(productInfo)))
		}
		if id := ctx.Send(messages).ID(); id == 0 {
			ctx.Send(message.Text("发送商店失败"))
			return
		}

		ctx.SendChain(message.Text("输入对应序号进行购买商品"))
		recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.CheckGroup(gid), zero.RegexRule(`^(\d+)$`)).Repeat()
		defer cancel()
		timer := time.NewTimer(120 * time.Second)
		answer := ""
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				ctx.SendChain(message.At(uid), message.Text(" 超时,已自动取消"))
				return
			case r := <-recv:
				answer = r.Event.Message.String()
				n, err := strconv.Atoi(answer)
				if err != nil {
					ctx.SendChain(message.Text("ERROR: ", err))
					return
				}

				if err = niu.Store(gid, uid, n); err != nil {
					ctx.SendChain(message.Text("ERROR: ", err))
					return
				}

				ctx.SendChain(message.Text("购买成功!"))
				return
			}
		}
	})
	en.OnFullMatch("赎牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		last, ok := jjCount.Load(fmt.Sprintf("%d_%d", gid, uid))

		if !ok {
			ctx.SendChain(message.Text("你还没有被厥呢"))
			return
		}

		if time.Now().Unix()-last.TimeLimit > 60*60 {
			ctx.SendChain(message.Text("时间已经过期了,牛牛已被收回!"))
			jjCount.Delete(fmt.Sprintf("%d_%d", gid, uid))
			return
		}

		if last.Count < 4 {
			ctx.SendChain(message.Text("你还没有被厥够4次呢,不能赎牛牛"))
			return
		}
		ctx.SendChain(message.Text("再次确认一下哦,这次赎牛牛，牛牛长度将会变成", last.Length, "cm\n还需要嘛【是|否】"))
		recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.CheckGroup(gid), zero.RegexRule(`^(是|否)$`)).Repeat()
		defer cancel()
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				ctx.SendChain(message.Text("操作超时，已自动取消"))
				return
			case c := <-recv:
				answer := c.Event.Message.String()
				if answer == "否" {
					ctx.SendChain(message.Text("取消成功!"))
					return
				}

				if err := niu.Redeem(gid, uid, last.Length); err == nil {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}

				jjCount.Delete(fmt.Sprintf("%d_%d", gid, uid))

				ctx.SendChain(message.At(uid), message.Text(fmt.Sprintf("恭喜你!成功赎回牛牛,当前长度为:%.2fcm", last.Length)))
				return
			}
		}
	})
	en.OnFullMatch("牛子长度排行", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		infos, err := niu.GetRankingInfo(gid, true)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		img, err := processRankingImg(infos, ctx, true)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.ImageBytes(img))
	})
	en.OnFullMatch("牛子深度排行", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		infos, err := niu.GetRankingInfo(gid, false)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		img, err := processRankingImg(infos, ctx, false)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.ImageBytes(img))
	})
	en.OnFullMatch("查看我的牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		gid := ctx.Event.GroupID
		view, err := niu.View(gid, uid, ctx.CardOrNickName(uid))
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(view))
	})
	en.OnRegex(`^(?:.*使用(.*))??打胶$`, zero.OnlyGroup).SetBlock(true).Limit(func(ctx *zero.Ctx) *rate.Limiter {
		lt := dajiaoLimiter.Load(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
		ctx.State["dajiao_last_touch"] = lt.LastTouch()
		return lt
	}, func(ctx *zero.Ctx) {
		timePass := int(time.Since(time.Unix(ctx.State["dajiao_last_touch"].(int64), 0)).Seconds())
		ctx.SendChain(message.Text(randomChoice([]string{
			fmt.Sprintf("才过去了%ds时间,你就又要打🦶了，身体受得住吗", timePass),
			fmt.Sprintf("不行不行，你的身体会受不了的，歇%ds再来吧", 90-timePass),
			fmt.Sprintf("休息一下吧，会炸膛的！%ds后再来吧", 90-timePass),
			fmt.Sprintf("打咩哟，你的牛牛会爆炸的，休息%ds再来吧", 90-timePass),
		})))
	}).Handle(func(ctx *zero.Ctx) {
		// 获取群号和用户ID
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		fiancee := ctx.State["regex_matched"].([]string)

		msg, err := niu.HitGlue(gid, uid, fiancee[1])
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			dajiaoLimiter.Delete(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
	})
	en.OnFullMatch("注册牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		msg, err := niu.Register(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
	})
	en.OnRegex(`^(?:.*使用(.*))??jj\s?(\[CQ:at,(?:\S*,)?qq=(\d+)(?:,\S*)?\]|(\d+))$`,
		zero.OnlyGroup).SetBlock(true).Limit(func(ctx *zero.Ctx) *rate.Limiter {
		lt := jjLimiter.Load(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
		ctx.State["jj_last_touch"] = lt.LastTouch()
		return lt
	}, func(ctx *zero.Ctx) {
		timePass := int(time.Since(time.Unix(ctx.State["jj_last_touch"].(int64), 0)).Seconds())
		ctx.SendChain(message.Text(randomChoice([]string{
			fmt.Sprintf("才过去了%ds时间,你就又要击剑了，真是饥渴难耐啊", timePass),
			fmt.Sprintf("不行不行，你的身体会受不了的，歇%ds再来吧", 150-timePass),
			fmt.Sprintf("你这种男同就应该被送去集中营！等待%ds再来吧", 150-timePass),
			fmt.Sprintf("打咩哟！你的牛牛会炸的，休息%ds再来吧", 150-timePass),
		})))
	},
	).Handle(func(ctx *zero.Ctx) {
		fiancee := ctx.State["regex_matched"].([]string)
		adduser, err := strconv.ParseInt(fiancee[3]+fiancee[4], 10, 64)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			jjLimiter.Delete(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
			return
		}
		uid := ctx.Event.UserID
		gid := ctx.Event.GroupID
		msg, length, err := niu.JJ(gid, uid, adduser, fiancee[1])
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			jjLimiter.Delete(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
		j := fmt.Sprintf("%d_%d", gid, adduser)
		count, ok := jjCount.Load(j)
		var c lastLength
		// 按照最后一次被jj时的时间计算，超过60分钟则重置
		if !ok {
			c = lastLength{
				TimeLimit: time.Now().Unix(),
				Count:     1,
				Length:    length,
			}
		} else {
			c = lastLength{
				TimeLimit: time.Now().Unix(),
				Count:     count.Count + 1,
				Length:    count.Length,
			}
			if time.Now().Unix()-count.TimeLimit > 60*60 {
				c = lastLength{
					TimeLimit: time.Now().Unix(),
					Count:     1,
					Length:    length,
				}
			}
		}

		jjCount.Store(j, &c)
		if c.Count > 2 {
			ctx.SendChain(message.Text(randomChoice([]string{
				fmt.Sprintf("你们太厉害了，对方已经被你们打了%d次了，你们可以继续找他🤺", c.Count),
				"你们不要再找ta🤺啦！"},
			)))

			if c.Count >= 4 {
				id := ctx.SendPrivateMessage(adduser,
					message.Text(fmt.Sprintf("你在%d群里已经被厥冒烟了，快去群里赎回你原本的牛牛!\n发送:`赎牛牛`即可！", gid)))
				if id == 0 {
					ctx.SendChain(message.At(adduser), message.Text("快发送`赎牛牛`来赎回你原本的牛牛!"))
				}
			}
		}
	})
	en.OnFullMatch("注销牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		gid := ctx.Event.GroupID
		msg, err := niu.Cancel(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
	})
}
