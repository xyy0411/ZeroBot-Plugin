package matching

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type matchSuccessEvent struct {
	ctx           *zero.Ctx
	userID        int64
	wsMsg         string
	matchedUserID int64
	matchID       string
}

var (
	matchSuccessWorkerOnce sync.Once
	matchSuccessEventCh    = make(chan matchSuccessEvent, 64)
)

func startMatchSuccessWorker() {
	matchSuccessWorkerOnce.Do(func() {
		go func() {
			processedMatchID := make(map[string]time.Time)
			ticker := time.NewTicker(15 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case ev := <-matchSuccessEventCh:
					if ev.matchID == "" {
						ev.ctx.SendPrivateMessage(ev.userID, message.Text("匹配成功，但服务端未返回唯一对局ID（match_id），无法开启转发聊天，请联系管理员修复"))
						continue
					}
					// 这样可以确保同一匹配的两个用户都能正确处理
					eventKey := ev.matchID
					if expiredAt, ok := processedMatchID[eventKey]; ok && time.Now().Before(expiredAt) {
						// 即使已经处理过，也要确保双方都有转发会话
						handleMatchSuccessEvent(ev)
						continue
					}
					processedMatchID[eventKey] = time.Now().Add(defaultForwardDuration)
					handleMatchSuccessEvent(ev)
				case <-ticker.C:
					now := time.Now()
					for matchID, expiredAt := range processedMatchID {
						if now.After(expiredAt) {
							delete(processedMatchID, matchID)
						}
					}
				}
			}
		}()
	})
}

func handleMatchSuccessEvent(ev matchSuccessEvent) {
	if ev.matchedUserID == 0 {
		return
	}

	var msg strings.Builder
	msg.WriteString("匹配成功\n当前状态:")
	defer func() {
		msg.WriteString("\n如想知道我的所有功能可发送 `/用法matching`")
		msg.WriteString("\n延迟转发聊天可发送`延长转发聊天 [分钟]`")
		ev.ctx.SendPrivateMessage(ev.userID, msg.String())
	}()

	masterUserIsBotFriend := canForwardPrivateMessage(ev.ctx, ev.userID)
	matchedUserIsBotFriend := canForwardPrivateMessage(ev.ctx, ev.matchedUserID)

	switch {
	case masterUserIsBotFriend && matchedUserIsBotFriend:
		msg.WriteString("双方都是好友，可直接转发聊天")
	case masterUserIsBotFriend && !matchedUserIsBotFriend:
		msg.WriteString("你已是机器人好友，但对方可能不是（或好友列表未刷新），你仍可尝试转发聊天")
	case !masterUserIsBotFriend && matchedUserIsBotFriend:
		msg.WriteString("你暂不是机器人好友（或好友列表未刷新），但你仍可尝试转发聊天")
	default:
		msg.WriteString("双方都可能不是机器人好友（或好友列表未刷新），2小时转发聊天可能失败，但可先尝试")
	}

	if !masterUserIsBotFriend || !matchedUserIsBotFriend {
		msg.WriteString("\n如果您未接收到任何对手的消息 可能是转发失败 请您直接通过qq号添加对手进行聊天")
	}

	msg.WriteString("\n你发给机器人的私聊消息将全部转发给匹配成功的用户；可发送 `关闭转发聊天` 主动结束。")

	// 检查双方是否已经存在转发会话
	peerID, hasSession := getForwardPeer(ev.userID)
	matchedPeerID, matchedHasSession := getForwardPeer(ev.matchedUserID)

	if hasSession && matchedHasSession && peerID == ev.matchedUserID && matchedPeerID == ev.userID {
		// 双方都已存在转发会话，直接返回
		msg.WriteString("\n已开启2小时转发聊天")
		return
	}

	// 注册双向转发会话
	_ = registerForwardSession(ev.userID, ev.matchedUserID, defaultForwardDuration)

	// 再次检查是否成功注册或已存在会话
	if peerID, ok := getForwardPeer(ev.userID); ok && peerID == ev.matchedUserID {
		msg.WriteString("\n已开启2小时转发聊天")
	} else {
		msg.WriteString("\n开启转发聊天失败，请重新匹配")
	}
}

func enqueueMatchSuccessNotice(ctx *zero.Ctx, userID int64, wsMsg string, matchedUserID int64, matchID string, isMatchSuccess bool) {
	if !isMatchSuccess && !strings.Contains(wsMsg, "匹配成功") {
		return
	}
	matchSuccessEventCh <- matchSuccessEvent{
		ctx:           ctx,
		userID:        userID,
		wsMsg:         wsMsg,
		matchedUserID: matchedUserID,
		matchID:       matchID,
	}
}

func processMatching(ctx *zero.Ctx, userID int64) {
	if _, ok := getForwardPeer(userID); ok {
		ctx.SendChain(message.Text("你当前正在进行转发聊天，请先发送“关闭转发聊天”后再开始匹配"))
		return
	}

	inQueue, queueMsg, err := isUserInMatchingQueue(userID)
	if err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}
	if inQueue {
		if queueMsg == "" {
			queueMsg = "你已经在匹配队列中了，无需重复开始匹配"
		}
		ctx.SendChain(message.Text(queueMsg))
		return
	}

	var dl websocket.Dialer
	conn, _, err := dl.Dial(fmt.Sprintf("ws://127.0.0.1:3000/api/matching/%d", userID), nil)
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

		rawMsg := string(msg)
		displayMsg, matchedUserID, matchID, isMatchSuccess := parseMatchWSMessage(msg)
		enqueueMatchSuccessNotice(ctx, userID, rawMsg, matchedUserID, matchID, isMatchSuccess)
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(displayMsg))
	}
}

func parseMatchWSMessage(raw []byte) (displayMsg string, matchedUserID int64, matchID string, isMatchSuccess bool) {
	rawText := string(raw)
	displayMsg = rawText

	var payload matchWSPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return displayMsg, 0, "", strings.Contains(rawText, "匹配成功")
	}
	if payload.Message != "" {
		displayMsg = payload.Message
	}
	if payload.PeerID != 0 {
		matchedUserID = payload.PeerID
	}
	matchID = payload.MatchID
	if payload.Type == "match_success" {
		isMatchSuccess = true
	}
	if !isMatchSuccess && strings.Contains(displayMsg, "匹配成功") {
		isMatchSuccess = true
	}
	return displayMsg, matchedUserID, matchID, isMatchSuccess
}

func canForwardPrivateMessage(ctx *zero.Ctx, matchedID int64) bool {
	if matchedID == 0 {
		return false
	}
	friendIDs := getFriendIDSet(ctx)
	_, ok := friendIDs[matchedID]
	return ok
}

func getFriendIDSet(ctx *zero.Ctx) map[int64]struct{} {
	friendIDs := make(map[int64]struct{})
	for _, friend := range ctx.GetFriendList().Array() {
		id := friend.Get("user_id").Int()
		if id != 0 {
			friendIDs[id] = struct{}{}
		}
	}
	return friendIDs
}

func registerForwardSession(uid, peerID int64, duration time.Duration) bool {
	expiresAt := time.Now().Add(duration)
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	if hasActiveForwardSessionLocked(uid) || hasActiveForwardSessionLocked(peerID) {
		return false
	}
	forwardSessions[uid] = forwardSession{PeerID: peerID, ExpiresAt: expiresAt}
	forwardSessions[peerID] = forwardSession{PeerID: uid, ExpiresAt: expiresAt}
	delete(forwardExpiredNotices, uid)
	delete(forwardExpiredNotices, peerID)
	return true
}

func hasActiveForwardSessionLocked(uid int64) bool {
	session, ok := forwardSessions[uid]
	if !ok {
		return false
	}
	if time.Now().After(session.ExpiresAt) {
		delete(forwardSessions, uid)
		if peerSession, exists := forwardSessions[session.PeerID]; exists && peerSession.PeerID == uid {
			delete(forwardSessions, session.PeerID)
		}
		return false
	}
	return true
}

func getForwardPeer(uid int64) (int64, bool) {
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	session, ok := forwardSessions[uid]
	if !ok {
		return 0, false
	}
	if time.Now().After(session.ExpiresAt) {
		delete(forwardSessions, uid)
		if peerSession, exists := forwardSessions[session.PeerID]; exists && peerSession.PeerID == uid {
			delete(forwardSessions, session.PeerID)
		}
		return 0, false
	}
	return session.PeerID, true
}

func unregisterForwardSession(uid int64) (int64, bool) {
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	session, ok := forwardSessions[uid]
	if !ok {
		return 0, false
	}
	delete(forwardSessions, uid)
	if peerSession, exists := forwardSessions[session.PeerID]; exists && peerSession.PeerID == uid {
		delete(forwardSessions, session.PeerID)
	}
	return session.PeerID, true
}

func extendForwardSession(uid int64, duration time.Duration) (int64, time.Time, bool) {
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	if duration <= 0 {
		return 0, time.Time{}, false
	}

	now := time.Now()
	session, ok := forwardSessions[uid]
	if !ok {
		return 0, time.Time{}, false
	}
	if now.After(session.ExpiresAt) {
		delete(forwardSessions, uid)
		if peerSession, exists := forwardSessions[session.PeerID]; exists && peerSession.PeerID == uid {
			delete(forwardSessions, session.PeerID)
		}
		return 0, time.Time{}, false
	}

	peerSession, exists := forwardSessions[session.PeerID]
	if !exists || peerSession.PeerID != uid || now.After(peerSession.ExpiresAt) {
		delete(forwardSessions, uid)
		if exists {
			delete(forwardSessions, session.PeerID)
		}
		return 0, time.Time{}, false
	}

	newExpiresAt := session.ExpiresAt.Add(duration)
	forwardSessions[uid] = forwardSession{PeerID: session.PeerID, ExpiresAt: newExpiresAt}
	forwardSessions[session.PeerID] = forwardSession{PeerID: uid, ExpiresAt: newExpiresAt}
	return session.PeerID, newExpiresAt, true
}

func apiURL(path string) string {
	return matchingAPIBase + path
}

func doMatchInfo(uid int64) (m Matching, err error) {
	resp, err := http.Get(apiURL("/profile/" + strconv.FormatInt(uid, 10)))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("status code: %d", resp.StatusCode)
	}
	all, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	m1 := make(map[string]any)
	err = json.Unmarshal(all, &m1)
	if err != nil {
		return
	}
	m2 := m1["data"].(map[string]any)["matching"]
	marshal, err := json.Marshal(m2)
	if err != nil {
		return
	}
	err = json.Unmarshal(marshal, &m)
	return
}

func doRequest(method, path string, body io.Reader, contentType string) (int, []byte, error) {
	req, err := http.NewRequest(method, apiURL(path), body)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, err
	}
	return res.StatusCode, respBody, nil
}

func doJSON(method, path string, payload any) (int, []byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	return doRequest(method, path, bytes.NewReader(data), "application/json")
}

func ensureProfile(userID int64, userName string, limitTime int64) error {

	_, err := doMatchInfo(userID)
	if err == nil {
		return err
	}
	status, body, err := doJSON(http.MethodPost, "/profile", map[string]any{
		"user_id":   userID,
		"user_name": userName,
		"expire_at": limitTime,
	})
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusConflict || status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		text := strings.ToLower(string(body))
		if strings.Contains(text, "exists") || strings.Contains(text, "已存在") {
			return nil
		}
	}
	return fmt.Errorf("create profile failed: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}

func updateExpire(userID int64, seconds int64) error {
	status, body, err := doJSON(http.MethodPatch, "/profile/"+strconv.FormatInt(userID, 10)+"/expire", map[string]any{
		"expire_at":  seconds,
		"limit_time": seconds,
		"expire":     seconds,
		"seconds":    seconds,
	})
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("更新匹配时间失败: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}

func addSoftware(userID int64, software string, softwareType int8) error {
	status, body, err := doJSON(http.MethodPost, "/profile/"+strconv.FormatInt(userID, 10)+"/software", map[string]any{
		"name":          software,
		"software_name": software,
		"type":          softwareType,
	})
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("添加匹配软件失败: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}

func addBlockUser(userID, targetUserID int64) error {
	status, body, err := doJSON(http.MethodPost, "/profile/"+strconv.FormatInt(userID, 10)+"/block-user", map[string]any{
		"target_user_id": targetUserID,
		"bl_user":        targetUserID,
	})
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("add block-user failed: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}
