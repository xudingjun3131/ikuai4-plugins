package core

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
)

// AppSignature maps domain/SNI/Host patterns to an application and category.
// This is the "应用识别" layer: the idea from nDPI/OpenAppFilter is L7
// identification, but a full DPI engine needs NFQUEUE + kernel module + a
// signature library that cannot ship inside an iKuai plugin. We implement the
// pragmatic 80/20: match on the domain/SNI/Host that every modern app exposes
// even under TLS 1.3 / ECH-less deployments, with a user-editable signature
// table (same "custom signatures" strength OAF advertises).
type AppSignature struct {
	App      string   `json:"app"`
	Category Category `json:"category"`
	Domains  []string `json:"domains"` // suffix patterns, e.g. douyin.com
}

type AppID struct {
	mu         sync.RWMutex
	sigs       []AppSignature
	exact      map[string]AppSignature
	suffixList []suffixEntry
}

type suffixEntry struct {
	suffix string
	sig    AppSignature
}

func NewAppID() *AppID {
	a := &AppID{exact: map[string]AppSignature{}}
	for _, s := range defaultSignatures() {
		a.Add(s)
	}
	return a
}

func (a *AppID) Add(s AppSignature) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sigs = append(a.sigs, s)
	for _, raw := range s.Domains {
		d := normalizeDomain(raw)
		if d == "" {
			continue
		}
		if strings.HasPrefix(d, "*.") {
			d = strings.TrimPrefix(d, "*.")
		}
		a.exact[d] = s
		a.suffixList = append(a.suffixList, suffixEntry{suffix: d, sig: s})
	}
	sort.Slice(a.suffixList, func(i, j int) bool {
		return len(a.suffixList[i].suffix) > len(a.suffixList[j].suffix)
	})
}

// Classify returns app name + category for a domain. Empty app = unidentified.
func (a *AppID) Classify(domain string) (string, Category) {
	d := normalizeDomain(domain)
	a.mu.RLock()
	defer a.mu.RUnlock()
	if s, ok := a.exact[d]; ok {
		return s.App, s.Category
	}
	for _, e := range a.suffixList {
		if d == e.suffix || strings.HasSuffix(d, "."+e.suffix) {
			return e.sig.App, e.sig.Category
		}
	}
	return "", CatUnknown
}

func (a *AppID) LoadFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var sigs []AppSignature
	if err := json.Unmarshal(b, &sigs); err != nil {
		return err
	}
	for _, s := range sigs {
		a.Add(s)
	}
	return nil
}

func (a *AppID) List() []AppSignature {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]AppSignature, len(a.sigs))
	copy(out, a.sigs)
	return out
}

// defaultSignatures is a small built-in starter table; real deployments load
// a larger community signature file into the config dir (app-signatures.json).
func defaultSignatures() []AppSignature {
	return []AppSignature{
		{"抖音", CatVideo, []string{"douyin.com", "douyinvod.com", "byteimg.com", "snssdk.com", "amemv.com"}},
		{"快手", CatVideo, []string{"kuaishou.com", "gifshow.com", "kwai.com"}},
		{"B站", CatVideo, []string{"bilibili.com", "bilivideo.com", "hdslb.com"}},
		{"爱奇艺", CatVideo, []string{"iqiyi.com", "qiyi.com", "iqiyipic.com"}},
		{"腾讯视频", CatVideo, []string{"qq.com", "qqlive.com", "video.qq.com", "myqcloud.com"}},
		{"微信", CatSocial, []string{"weixin.qq.com", "wechat.com", "wx.qq.com"}},
		{"QQ", CatSocial, []string{"qq.com", "qzone.com"}},
		{"微博", CatSocial, []string{"weibo.com", "weibocdn.com", "sina.cn"}},
		{"王者荣耀", CatGame, []string{"pvp.qq.com", "tencentgames.com"}},
		{"Steam", CatGame, []string{"steampowered.com", "steamcommunity.com", "steamstatic.com"}},
		{"迅雷", CatP2P, []string{"xunlei.com", "xl7.xunlei.com"}},
		{"BT/P2P", CatP2P, []string{"tracker", "bittorrent.com"}},
		{"VPN/代理", CatVPN, []string{"clash", "v2ray", "openvpn", "wireguard"}},
		{"企业办公", CatWork, []string{"feishu.cn", "dingtalk.com", "wework.com", "office.com", "microsoft.com"}},
	}
}
