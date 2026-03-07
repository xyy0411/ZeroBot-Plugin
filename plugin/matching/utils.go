package matching

import (
	"sync"
	"time"
)

var forwardExpiryWorkerOnce sync.Once

func init() {
	startForwardExpiryWorker()
}

func startForwardExpiryWorker() {
	forwardExpiryWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				expiredPairs := popExpiredForwardPairs()
				forwardSessionMu.Lock()
				for _, pair := range expiredPairs {
					forwardExpiredNotices[pair[0]] = struct{}{}
					forwardExpiredNotices[pair[1]] = struct{}{}
				}
				forwardSessionMu.Unlock()
			}
		}()
	})
}

func popExpiredForwardPairs() [][2]int64 {
	now := time.Now()
	pairs := make([][2]int64, 0, 4)
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()

	for uid, session := range forwardSessions {
		peerSession, ok := forwardSessions[session.PeerID]
		if !ok || peerSession.PeerID != uid {
			continue
		}
		if now.Before(session.ExpiresAt) && now.Before(peerSession.ExpiresAt) {
			continue
		}
		if uid < session.PeerID {
			pairs = append(pairs, [2]int64{uid, session.PeerID})
		}
		delete(forwardSessions, uid)
		delete(forwardSessions, session.PeerID)
	}
	return pairs
}

func consumeForwardExpiredNotice(uid int64) bool {
	if uid == 0 {
		return false
	}
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	if _, ok := forwardExpiredNotices[uid]; !ok {
		return false
	}
	delete(forwardExpiredNotices, uid)
	return true
}
