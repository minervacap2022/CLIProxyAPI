// Package thinking provides unified thinking configuration processing.
package thinking

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// SanitizeMessagesThinking removes thinking blocks with empty, missing, or non-string
// signatures from every assistant message's content array, and strips the proxy-injected
// "signature" field from tool_use blocks.
//
// This prevents Anthropic's API from returning a 400
// "Invalid `signature` in `thinking` block" error when a conversation history contains
// thinking blocks produced by non-Claude providers (e.g. Kimi, OpenAI-compatible) whose
// response translators emit thinking blocks without valid signatures.
//
// The function is a no-op when body is empty, invalid JSON, or contains no messages array.
func SanitizeMessagesThinking(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}

	modified := false
	for msgIdx, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			continue
		}

		var keepBlocks []interface{}
		contentModified := false

		for _, block := range content.Array() {
			blockType := block.Get("type").String()
			if blockType == "thinking" {
				sig := block.Get("signature")
				if !sig.Exists() || sig.Type != gjson.String || strings.TrimSpace(sig.String()) == "" {
					contentModified = true
					continue
				}
			}

			// Preserve raw JSON to avoid float64 rounding of large integers in tool_use inputs.
			blockRaw := []byte(block.Raw)
			if blockType == "tool_use" && block.Get("signature").Exists() {
				blockRaw, _ = sjson.DeleteBytes(blockRaw, "signature")
				contentModified = true
			}

			keepBlocks = append(keepBlocks, json.RawMessage(blockRaw))
		}

		if contentModified {
			contentPath := fmt.Sprintf("messages.%d.content", msgIdx)
			var err error
			if len(keepBlocks) == 0 {
				body, err = sjson.SetBytes(body, contentPath, []interface{}{})
			} else {
				body, err = sjson.SetBytes(body, contentPath, keepBlocks)
			}
			if err != nil {
				log.Warnf("thinking sanitize: failed to sanitize message %d: %v", msgIdx, err)
				continue
			}
			modified = true
		}
	}

	if modified {
		log.Debug("thinking sanitize: removed thinking blocks with invalid signatures")
	}
	return body
}
