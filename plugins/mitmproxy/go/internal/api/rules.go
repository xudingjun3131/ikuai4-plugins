package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GitterBois/proxying/internal/rules"
)

func rulesCollection(chain *rules.Chain, onChange func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if chain == nil {
			http.Error(w, "rules unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"rules": chain.List()})
		case http.MethodPost:
			var rule rules.Rule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if rule.ID == "" {
				rule.ID = newID()
			}
			if rule.Type == "" {
				http.Error(w, "type required", http.StatusBadRequest)
				return
			}
			list := chain.List()
			list = append(list, rule)
			if err := chain.Save(list); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if onChange != nil {
				onChange()
			}
			writeJSON(w, http.StatusCreated, rule)
		case http.MethodPut:
			// 载入内置规则集(规则面板「内置规则」按钮): 整包替换当前规则。
			// 停用/删掉的内置规则想找回来就走这条, 不用重装。
			if err := chain.Save(rules.DefaultRules()); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if onChange != nil {
				onChange()
			}
			writeJSON(w, http.StatusOK, map[string]any{"rules": chain.List(), "loaded": "builtin"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func rulesItem(chain *rules.Chain, onChange func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if chain == nil {
			http.Error(w, "rules unavailable", http.StatusServiceUnavailable)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/rules/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		list := chain.List()
		idx := -1
		for i := range list {
			if list[i].ID == id {
				idx = i
				break
			}
		}
		switch r.Method {
		case http.MethodGet:
			if idx < 0 {
				http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, list[idx])
		case http.MethodPut:
			var rule rules.Rule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			rule.ID = id
			if idx < 0 {
				list = append(list, rule)
			} else {
				list[idx] = rule
			}
			if err := chain.Save(list); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if onChange != nil {
				onChange()
			}
			writeJSON(w, http.StatusOK, rule)
		case http.MethodDelete:
			if idx < 0 {
				http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
				return
			}
			list = append(list[:idx], list[idx+1:]...)
			if err := chain.Save(list); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if onChange != nil {
				onChange()
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
