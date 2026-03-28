package matching

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const shutdownForwardNotice = "机器人维护中，即将断开转发聊天"

func init() {
	registerShutdownNotice()
}

func registerShutdownNotice() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		notifyForwardSessionsOnShutdown()
	}()
}

func notifyForwardSessionsOnShutdown() {
	ctx := getAnyBotCtx()
	if ctx == nil {
		return
	}

	pairs := forwardManager.CloseAll()
	for _, pair := range pairs {
		sendShutdownNotice(ctx, pair.UserID)
		sendShutdownNotice(ctx, pair.PeerID)
	}
}

func getAnyBotCtx() *zero.Ctx {
	var ctx *zero.Ctx
	zero.RangeBot(func(_ int64, botCtx *zero.Ctx) bool {
		ctx = botCtx
		return false
	})
	return ctx
}

func sendShutdownNotice(ctx *zero.Ctx, uid int64) {
	if uid == 0 {
		return
	}

	ctx.SendPrivateMessage(uid, message.Text(shutdownForwardNotice))
	time.Sleep(150 * time.Millisecond)
}
