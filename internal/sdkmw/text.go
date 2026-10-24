package sdkmw

import (
	"encoding/json"
	"strings"
)

// Text returns the text in a message's content, for estimating its tokens.
// Content may be a string, an array of parts, or a part object with "text"
// or nested "content". Anything else, such as images, counts as no text.
func Text(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		texts := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := Text(p); t != "" {
				texts = append(texts, t)
			}
		}
		return strings.Join(texts, "\n")
	}
	var part struct {
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &part) == nil {
		if t := Text(part.Content); t != "" {
			return part.Text + t
		}
		return part.Text
	}
	return ""
}
