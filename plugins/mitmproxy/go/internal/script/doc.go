// Package script runs sandboxed JS (goja) rewrite scripts on proxy traffic.
//
// Scripts live under $DATA/scripts/*.js and may define:
//
//	function onRequest(flow)  { /* mutate flow.request */ }
//	function onResponse(flow) { /* mutate flow.response */ }
//
// Allowlisted fields: request.{method,url,headers,body}, response.{status,headers,body}.
// Execution is hard-capped by ScriptTimeout (default 100ms); infinite loops are interrupted.
package script
