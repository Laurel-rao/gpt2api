package ecommerce

import (
	"encoding/json"
	"strings"
)

type deltaExtractor struct {
	lastFull  string
	curP      string
	recipient string
}

func (d *deltaExtractor) Extract(data []byte) (string, bool) {
	if d.recipient == "" {
		d.recipient = "all"
	}
	if strings.TrimSpace(string(data)) == "[DONE]" {
		return "", true
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false
	}
	if t, _ := raw["type"].(string); t == "message_stream_complete" {
		return "", true
	}
	if p, ok := raw["p"].(string); ok {
		d.curP = p
	}
	if strings.HasPrefix(d.curP, "/message/content/thoughts") {
		return "", false
	}
	v, hasV := raw["v"]
	if !hasV {
		return "", false
	}
	if s, ok := v.(string); ok {
		if d.curP == "/message/status" && s == "finished_successfully" {
			return "", true
		}
		if d.recipient == "all" && (d.curP == "" || d.curP == "/message/content/parts/0") {
			return s, false
		}
		return "", false
	}
	if arr, ok := v.([]interface{}); ok {
		var b strings.Builder
		final := false
		for _, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if p, _ := m["p"].(string); p != "" {
				d.curP = p
			}
			if d.curP == "/message/status" {
				if s, ok := m["v"].(string); ok && s == "finished_successfully" {
					final = true
				}
				continue
			}
			if d.recipient == "all" && (d.curP == "" || d.curP == "/message/content/parts/0") {
				if s, ok := m["v"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String(), final
	}
	if m, ok := v.(map[string]interface{}); ok {
		if msg, ok := m["message"].(map[string]interface{}); ok {
			if r, ok := msg["recipient"].(string); ok && r != "" {
				d.recipient = r
			}
		}
	}
	if msg, ok := raw["message"].(map[string]interface{}); ok {
		if r, ok := msg["recipient"].(string); ok && r != "" {
			d.recipient = r
		}
		if content, ok := msg["content"].(map[string]interface{}); ok {
			if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
				if cur, ok := parts[0].(string); ok {
					delta := cur
					if strings.HasPrefix(cur, d.lastFull) {
						delta = cur[len(d.lastFull):]
					}
					d.lastFull = cur
					final := false
					if status, _ := msg["status"].(string); status == "finished_successfully" {
						final = true
					}
					if d.recipient != "all" {
						return "", final
					}
					return delta, final
				}
			}
		}
	}
	return "", false
}
