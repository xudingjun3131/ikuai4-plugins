package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GitterBois/runhooks/internal/subscription"
)

// subscriptionsCollection: GET 列表 / POST 新增(立刻拉取解析生效)。
func subscriptionsCollection(m *subscription.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m == nil {
			http.Error(w, "subscription unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			patch, ruleN, scriptN := m.Effective()
			// [nil-slice] Go 的 nil slice 编成 null, 前端 .length/.map 直接崩
			// (React 整个 app 白屏、Vue 面板失灵)——归一成 [] 再出站
			if patch.Domains == nil {
				patch.Domains = []string{}
			}
			if patch.DstIPs == nil {
				patch.DstIPs = []string{}
			}
			if patch.Countries == nil {
				patch.Countries = []string{}
			}
			if patch.Paths == nil {
				patch.Paths = []string{}
			}
			if patch.UserAgents == nil {
				patch.UserAgents = []string{}
			}
			list := m.List()
			if list == nil {
				list = []subscription.Sub{}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"subs": list,
				"effective": map[string]any{
					"domains":     patch.Domains,
					"dst_ips":     patch.DstIPs,
					"countries":   patch.Countries,
					"paths":       patch.Paths,
					"user_agents": patch.UserAgents,
					"rules":       ruleN,
					"scripts":     scriptN,
				},
			})
		case http.MethodPost:
			var in struct {
				URL         string             `json:"url"`
				Name        string             `json:"name"`
				Format      subscription.Format `json:"format"`
				IntervalMin int                `json:"interval_min"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			sub, err := m.Add(in.URL, in.Name, in.Format, in.IntervalMin)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, sub)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// subscriptionsItem: PUT 改开关/备注/间隔, DELETE 删除, GET 解析明细, POST /update 立即更新。
func subscriptionsItem(m *subscription.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m == nil {
			http.Error(w, "subscription unavailable", http.StatusServiceUnavailable)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/subscriptions/")
		if rest == "" {
			http.NotFound(w, r)
			return
		}
		id := rest
		action := ""
		if i := strings.Index(rest, "/"); i >= 0 {
			id, action = rest[:i], rest[i+1:]
		}
		if strings.Contains(id, "/") || strings.Contains(action, "/") {
			http.NotFound(w, r)
			return
		}
		switch {
		case r.Method == http.MethodDelete:
			if err := m.Remove(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"removed": id})
		case r.Method == http.MethodPut && action == "":
			var in struct {
				Enabled     *bool   `json:"enabled"`
				Name        *string `json:"name"`
				IntervalMin *int    `json:"interval_min"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if in.Enabled != nil {
				if err := m.SetEnabled(id, *in.Enabled); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if in.Name != nil || in.IntervalMin != nil {
				name := ""
				interval := -1
				if in.Name != nil {
					name = *in.Name
				}
				if in.IntervalMin != nil {
					interval = *in.IntervalMin
				}
				if err := m.SetMeta(id, name, interval); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			sub, ok := m.Get(id)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, sub)
		case r.Method == http.MethodPost && action == "update":
			if err := m.Update(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			sub, _ := m.Get(id)
			writeJSON(w, http.StatusOK, sub)
		case r.Method == http.MethodGet && action == "parsed":
			p, err := m.ParsedOf(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, p)
		case r.Method == http.MethodGet && action == "raw":
			raw, err := m.RawOf(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"raw": raw})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
