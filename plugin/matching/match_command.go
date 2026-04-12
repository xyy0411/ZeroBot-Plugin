package matching

import (
	"fmt"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func handleRemoveRejectedMatchUser(uid int64) (string, error) {
	if err := removeRejectedMatchUser(uid); err != nil {
		return "", err
	}
	return "已退出被动匹配黑名单", nil
}

func handlePassiveMatchingPrompt(ctx *zero.Ctx) {
	uid := ctx.Event.UserID

	resp, err := http.Get(apiURL("/status/" + strconv.FormatInt(uid, 10)))
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return
		}
	}

	rejected, err := isRejectedMatchUser(uid)
	if err != nil || rejected {
		return
	}

	ctx.SendChain(message.Text("要我帮你找人联机吗?[是|否]"))
	recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.RegexRule(`(是|否|td|退出)$`)).Repeat()
	defer cancel()

	for {
		select {
		case <-time.After(2 * time.Minute):
			return
		case r := <-recv:
			if r.Event.Message.String() != "是" {
				if err := addRejectedMatchUser(uid); err != nil {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
				ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("好哦，以后就不打扰你啦"))
				return
			}
			if _, err := doMatchInfo(uid); err != nil {
				if err = ensureProfile(uid, ctx.CardOrNickName(uid), defaultProfileExpirePrompt); err != nil {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
			}
			forwardManager.processMatching(ctx, uid)
			return
		}
	}
}

func handleViewMatchingStatus(uid int64) (string, error) {
	_, body, err := doRequest(http.MethodGet, "/status/"+strconv.FormatInt(uid, 10), nil, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func handleViewMatchingExpire(uid int64) (string, error) {
	m, err := doMatchInfo(uid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("匹配时间为 %d 分钟", m.ExpireAt/60), nil
}

func handleCancelMatching(uid int64) (string, error) {
	msg, err := cancelMatching(uid)
	if err != nil {
		return "", err
	}
	if msg == "" {
		return "已取消匹配", nil
	}
	return msg, nil
}

func handleStartMatching(ctx *zero.Ctx) {
	uid := ctx.Event.UserID
	if err := ensureProfile(uid, ctx.CardOrNickName(uid), defaultProfileExpire); err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}
	forwardManager.processMatching(ctx, uid)
}
