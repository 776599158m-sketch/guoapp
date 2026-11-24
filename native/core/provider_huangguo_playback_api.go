package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 黄果 AI 的播放地址自 2026-11 起不再写进播放页 HTML，改由
// POST /api/videos/{id}/episodes/{ep}/playback 现场下发；页面脚本
// （services/playback.js）也是调这个接口。页面里的 videoSrc、videoApi、
// videoKey、videoIv、epPlaySrcs 现在一律为空，只保留 cdnAuthKey、
// cdnAuthValue、cdnM3u8Enabled 三个下发头部所需的字段。

const (
	huangguoAIPlaybackPath    = "/api/videos/%s/episodes/%s/playback"
	huangguoAIPlaybackTries   = 2
	huangguoAIPlaybackTimeout = 8 * time.Second
)

type huangguoAIPlaybackPayload struct {
	ID         string `json:"id"`
	Ep         int    `json:"ep"`
	MediaID    string `json:"media_id"`
	Mode       int    `json:"mode"`
	Timestamp  int64  `json:"timestamp"`
	Manifest   string `json:"manifest_method"`
	Streams    struct {
		Main    string `json:"main"`
		Hevc    string `json:"hevc"`
		Preview string `json:"preview"`
	} `json:"streams"`
	CDN struct {
		M3u8Enabled  bool   `json:"m3u8Enabled"`
		OtherEnabled bool   `json:"otherEnabled"`
		AuthKey      string `json:"authKey"`
		AuthValue    string `json:"authValue"`
		App          string `json:"app"`
	} `json:"cdn"`
}

type huangguoAIPlaybackEnvelope struct {
	Data   huangguoAIPlaybackPayload `json:"data"`
	Status int                       `json:"status"`
	Msg    string                    `json:"msg"`
}

// resolveHuangguoAIPlayback 按分集向站点接口取播放地址。它只读取站点自己
// 对匿名请求下发的 JSON，不构造签名、不携带账号凭据。
func (d *Downloader) resolveHuangguoAIPlayback(ctx context.Context, sourceID, episode, referer string) (providerMedia, error) {
	sourceID = strings.Trim(strings.TrimSpace(sourceID), "/")
	episode = strings.TrimSpace(episode)
	if sourceID == "" || episode == "" {
		return providerMedia{}, errors.New("黄果 AI 播放参数无效")
	}
	number, err := strconv.Atoi(episode)
	if err != nil || number < 1 {
		return providerMedia{}, errors.New("黄果 AI 分集编号无效")
	}
	path := fmt.Sprintf(huangguoAIPlaybackPath, url.PathEscape(sourceID), url.PathEscape(episode))
	base := strings.TrimRight(d.providerBaseURL(sourceHuangguoAI), "/")
	payloadURL := base + path
	candidates := d.orderProviderCandidates(payloadURL, d.providerURLCandidates(payloadURL), sourceHuangguoAI)
	if referer == "" {
		referer = base + "/"
	}
	var lastErr error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return providerMedia{}, err
		}
		payload, err := d.requestHuangguoAIPlayback(ctx, candidate, referer)
		if err != nil {
			lastErr = err
			continue
		}
		if payload.ID != "" && payload.ID != sourceID {
			lastErr = errors.New("黄果播放接口返回的剧集与所选不一致")
			continue
		}
		if payload.Ep != 0 && payload.Ep != number {
			lastErr = errors.New("黄果播放接口返回的分集与所选不一致")
			continue
		}
		media, err := d.huangguoAIPlaybackMedia(ctx, payload, candidate, referer)
		if err != nil {
			lastErr = err
			continue
		}
		d.providerMu.Lock()
		if parsed, parseErr := url.Parse(candidate); parseErr == nil {
			d.providerHosts[sourceHuangguoAI] = parsed.Scheme + "://" + parsed.Host
		}
		d.providerMu.Unlock()
		return media, nil
	}
	if lastErr == nil {
		lastErr = errors.New("黄果播放接口未返回可用地址")
	}
	return providerMedia{}, lastErr
}

func (d *Downloader) requestHuangguoAIPlayback(ctx context.Context, address, referer string) (huangguoAIPlaybackPayload, error) {
	var lastErr error
	for attempt := 1; attempt <= huangguoAIPlaybackTries; attempt++ {
		if err := ctx.Err(); err != nil {
			return huangguoAIPlaybackPayload{}, err
		}
		if attempt > 1 {
			select {
			case <-time.After(time.Duration(attempt) * time.Second):
			case <-ctx.Done():
				return huangguoAIPlaybackPayload{}, ctx.Err()
			}
		}
		payload, retry, err := d.sendHuangguoAIPlayback(ctx, address, referer)
		if err == nil {
			return payload, nil
		}
		lastErr = err
		if !retry {
			return huangguoAIPlaybackPayload{}, err
		}
	}
	return huangguoAIPlaybackPayload{}, lastErr
}

func (d *Downloader) sendHuangguoAIPlayback(ctx context.Context, address, referer string) (huangguoAIPlaybackPayload, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader([]byte("{}")))
	if err != nil {
		return huangguoAIPlaybackPayload{}, false, errors.New("黄果播放接口地址无效")
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	request.Header.Set("Origin", providerURLCandidateOrigin(address))
	request.Header.Set("Referer", providerRefererForURL(address, referer))
	response, err := d.doCatalogRequestWithTimeout(request, huangguoAIPlaybackTimeout)
	if err != nil {
		return huangguoAIPlaybackPayload{}, true, err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return huangguoAIPlaybackPayload{}, true, readErr
	}
	if closeErr != nil {
		return huangguoAIPlaybackPayload{}, true, closeErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// 4xx 是站点对这次请求的明确答复，换候选入口更有效，不原样重试。
		retry := response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests
		return huangguoAIPlaybackPayload{}, retry, fmt.Errorf("黄果播放接口返回 HTTP %d", response.StatusCode)
	}
	if len(body) > 1<<20 {
		return huangguoAIPlaybackPayload{}, false, errors.New("黄果播放接口响应过大")
	}
	var envelope huangguoAIPlaybackEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return huangguoAIPlaybackPayload{}, true, errors.New("黄果播放接口响应无法解析")
	}
	if envelope.Status != 1 {
		message := strings.TrimSpace(envelope.Msg)
		if message == "" {
			message = "播放参数无效"
		}
		return huangguoAIPlaybackPayload{}, false, fmt.Errorf("黄果播放接口未下发播放参数：%s", message)
	}
	payload := envelope.Data
	if payload.Manifest != "" && !strings.EqualFold(payload.Manifest, "GET") {
		return huangguoAIPlaybackPayload{}, false, errors.New("黄果播放接口下发了不支持的取流方式")
	}
	if payload.Timestamp > 0 && payload.Timestamp*1000 <= time.Now().Add(time.Minute).UnixMilli() {
		return huangguoAIPlaybackPayload{}, true, errors.New("黄果播放参数已过期")
	}
	if strings.TrimSpace(payload.Streams.Main) == "" {
		if strings.TrimSpace(payload.Streams.Preview) == "" {
			return huangguoAIPlaybackPayload{}, false, errors.New("黄果未下发该分集的播放地址")
		}
		return huangguoAIPlaybackPayload{}, false, errors.New("黄果仅下发预览地址，该分集当前没有可取播放流")
	}
	return payload, false, nil
}

func (d *Downloader) huangguoAIPlaybackMedia(ctx context.Context, payload huangguoAIPlaybackPayload, address, referer string) (providerMedia, error) {
	media := providerMedia{Referer: providerRefererForURL(address, referer)}
	media.URL = normalizeProviderMediaURL(address, payload.Streams.Main)
	if media.URL == "" {
		return providerMedia{}, errors.New("黄果播放接口下发的地址不是可用的媒体地址")
	}
	parsed, err := url.Parse(media.URL)
	if err != nil {
		return providerMedia{}, errors.New("黄果播放接口下发的地址无法解析")
	}
	if hevc := normalizeProviderMediaURL(address, payload.Streams.Hevc); hevc != "" && hevc != media.URL {
		media.Variants = append(media.Variants, providerMedia{URL: hevc, Referer: media.Referer})
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return media, nil
	}
	playlist, finalURL, err := d.fetchMediaPlaylist(ctx, media.URL, media.Referer)
	if err != nil {
		return providerMedia{}, fmt.Errorf("获取播放列表失败: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "#EXTM3U") {
		return providerMedia{}, errors.New("黄果播放列表无效，链接可能已失效")
	}
	if duration := m3u8Duration(playlist); duration > 0 {
		media.Duration = duration
	}
	media.Playlist = playlist
	media.URL = finalURL
	return media, nil
}
