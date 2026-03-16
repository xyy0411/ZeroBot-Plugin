package matching

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/FloatTech/gg"
	"github.com/FloatTech/imgfactory"
)

type matchingRecordsResp struct {
	Data matchingRecordsData `json:"data"`
}

type matchingRecordsData struct {
	Total   int              `json:"total"`
	Records []matchingRecord `json:"records"`
}

type matchingRecord struct {
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name"`
	PeerID   int64  `json:"peer_id"`
	PeerName string `json:"peer_name"`
}

type userUsage struct {
	Label string
	Count int
}

type matchingStatsDrawData struct {
	Title string
	Usage []userUsage
}

func buildTodayMatchingStatsChart() (string, []byte, error) {
	return buildMatchingStatsChart("/record/today", "当日匹配用户统计")
}

func buildAllMatchingStatsChart() (string, []byte, error) {
	return buildMatchingStatsChart("/record/all", "累计匹配用户统计")
}

func buildMatchingStatsChart(path, title string) (string, []byte, error) {
	total, records, err := fetchMatchingRecords(path)
	if err != nil {
		return "", nil, err
	}

	usage := aggregateUserUsage(records)
	summary := title + "\n匹配记录: " + strconv.Itoa(total) + " 条\n涉及用户: " + strconv.Itoa(len(usage)) + " 人"
	if len(usage) == 0 {
		return title + "\n暂无匹配记录", nil, nil
	}

	draw := matchingStatsDrawData{
		Title: title,
		Usage: usage,
	}
	img, err := drawMatchingStatsChart(draw)
	if err != nil {
		return "", nil, err
	}
	return summary, img, nil
}

func fetchMatchingRecords(path string) (int, []matchingRecord, error) {
	status, body, err := doRequest("GET", path, nil, "")
	if err != nil {
		return 0, nil, err
	}
	if status < 200 || status >= 300 {
		return 0, nil, fmt.Errorf("获取匹配统计失败: status=%d, body=%s", status, strings.TrimSpace(string(body)))
	}

	var resp matchingRecordsResp
	if err = json.Unmarshal(body, &resp); err != nil {
		return 0, nil, err
	}

	total := resp.Data.Total
	if total == 0 {
		total = len(resp.Data.Records)
	}
	return total, resp.Data.Records, nil
}

func aggregateUserUsage(records []matchingRecord) []userUsage {
	counts := make(map[string]int)
	for _, r := range records {
		if r.UserID > 0 {
			counts[userLabel(r.UserName, r.UserID)]++
		}
		if r.PeerID > 0 {
			counts[userLabel(r.PeerName, r.PeerID)]++
		}
	}

	result := make([]userUsage, 0, len(counts))
	for label, count := range counts {
		result = append(result, userUsage{Label: label, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Label < result[j].Label
		}
		return result[i].Count > result[j].Count
	})
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

func userLabel(name string, qqid int64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "未知用户"
	}
	id := strconv.FormatInt(qqid, 10)
	if len(id) > 6 {
		id = id[:2] + "***" + id[len(id)-2:]
	}
	return name + "(" + id + ")"
}

func drawMatchingStatsChart(draw matchingStatsDrawData) ([]byte, error) {
	const (
		w      = 1100
		h      = 620
		left   = 80.0
		right  = 40.0
		top    = 70.0
		bottom = 180.0
	)

	dc := gg.NewContext(w, h)
	dc.SetHexColor("#f8fafc")
	dc.Clear()

	plotW := float64(w) - left - right
	plotH := float64(h) - top - bottom

	dc.SetHexColor("#0f172a")
	dc.DrawStringAnchored(draw.Title, float64(w)/2, 30, 0.5, 0.5)

	maxValue := 5
	for _, item := range draw.Usage {
		if item.Count > maxValue {
			maxValue = item.Count
		}
	}
	scaleMax := int(math.Ceil(float64(maxValue)/5.0) * 5)
	if scaleMax == 0 {
		scaleMax = 5
	}

	dc.SetColor(color.RGBA{220, 226, 234, 255})
	dc.SetLineWidth(1)
	for i := 0; i <= 5; i++ {
		y := top + plotH*float64(i)/5
		dc.DrawLine(left, y, left+plotW, y)
		dc.Stroke()
		v := scaleMax - scaleMax*i/5
		dc.SetHexColor("#475569")
		dc.DrawStringAnchored(strconv.Itoa(v), left-8, y, 1, 0.5)
	}

	barGap := plotW / float64(len(draw.Usage)+1)
	barW := barGap * 0.6
	for i, item := range draw.Usage {
		x := left + barGap*float64(i+1)
		rate := float64(item.Count) / float64(scaleMax)
		barH := plotH * rate
		y := top + plotH - barH

		dc.SetHexColor("#6366f1")
		dc.DrawRoundedRectangle(x-barW/2, y, barW, barH, 8)
		dc.Fill()

		dc.SetHexColor("#0f172a")
		dc.DrawStringAnchored(strconv.Itoa(item.Count), x, y-8, 0.5, 1)

		dc.Push()
		dc.RotateAbout(-math.Pi/4, x, top+plotH+18)
		dc.SetHexColor("#334155")
		dc.DrawStringAnchored(item.Label, x, top+plotH+18, 0.5, 0)
		dc.Pop()
	}

	dc.SetHexColor("#334155")
	dc.DrawStringAnchored("X轴: 用户名(QQ号)", float64(w)/2, float64(h)-36, 0.5, 0.5)
	dc.DrawStringAnchored("Y轴: 使用次数", 26, top+plotH/2, 0.5, 0.5)

	return imgfactory.ToBytes(dc.Image())
}
