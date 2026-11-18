package proxy

import "net/http"

// RequestAction is the result of an OnRequest hook stage.
type RequestAction int

const (
	// RequestContinue forwards the (possibly mutated) request.
	RequestContinue RequestAction = iota
	// RequestDrop aborts with a synthetic error response.
	RequestDrop
	// RequestRespond short-circuits with the hook-provided response.
	RequestRespond
)

// ResponseAction is the result of an OnResponse hook stage.
type ResponseAction int

const (
	// ResponseContinue returns the (possibly mutated) response to the client.
	ResponseContinue ResponseAction = iota
	// ResponseDrop replaces the response with a synthetic error.
	ResponseDrop
)

// ProxyHooks is one interception stage (Rules, Script, or Breakpoint).
// Implementations must be safe for concurrent use.
type ProxyHooks interface {
	OnRequest(*http.Request) (*http.Request, *http.Response, RequestAction)
	OnResponse(*http.Response) (*http.Response, ResponseAction)
}

// HookSlots holds the three hook stages. A nil slot is a no-op.
//
// OnRequest order:  Rules → Script → Breakpoint
// OnResponse order: Breakpoint → Script → Rules
type HookSlots struct {
	Rules      ProxyHooks
	Script     ProxyHooks
	Breakpoint ProxyHooks
}

func (h HookSlots) runRequest(req *http.Request) (*http.Request, *http.Response) {
	for _, hook := range []ProxyHooks{h.Rules, h.Script, h.Breakpoint} {
		if hook == nil {
			continue
		}
		var resp *http.Response
		var action RequestAction
		req, resp, action = hook.OnRequest(req)
		switch action {
		case RequestDrop:
			return req, textResponse(req, http.StatusForbidden, "proxy: dropped by hook")
		case RequestRespond:
			if resp == nil {
				return req, textResponse(req, http.StatusForbidden, "proxy: dropped by hook")
			}
			return req, resp
		}
	}
	return req, nil
}

func (h HookSlots) runResponse(resp *http.Response) *http.Response {
	if resp == nil {
		return nil
	}
	req := resp.Request
	for _, hook := range []ProxyHooks{h.Breakpoint, h.Script, h.Rules} {
		if hook == nil {
			continue
		}
		var action ResponseAction
		resp, action = hook.OnResponse(resp)
		if action == ResponseDrop || resp == nil {
			return textResponse(req, http.StatusForbidden, "proxy: dropped by hook")
		}
	}
	return resp
}
