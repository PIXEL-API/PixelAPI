package service

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func collectRawResponsesOutputItemsFromSSE(bodyText string) ([]byte, bool) {
	var items []json.RawMessage
	seen := make(map[string]struct{})
	hasCompactionItem := false
	appendItem := func(item gjson.Result) {
		if !item.Exists() || !item.IsObject() {
			return
		}
		key := strings.TrimSpace(item.Get("id").String())
		if key == "" {
			key = item.Raw
		}
		if _, duplicate := seen[key]; duplicate {
			return
		}
		seen[key] = struct{}{}
		if isResponsesCompactionItemType(item.Get("type").String()) {
			hasCompactionItem = true
		}
		items = append(items, json.RawMessage(item.Raw))
	}
	forEachOpenAISSEDataPayload(bodyText, func(data []byte) {
		if normalized, changed := normalizeCompletedImageGenerationStatus(data); changed {
			data = normalized
		}
		if strings.TrimSpace(gjson.GetBytes(data, "type").String()) == "response.output_item.done" {
			appendItem(gjson.GetBytes(data, "item"))
		}
	})
	if !hasCompactionItem {
		forEachOpenAISSEDataPayload(bodyText, func(data []byte) {
			if strings.TrimSpace(gjson.GetBytes(data, "type").String()) != "response.output_item.added" {
				return
			}
			item := gjson.GetBytes(data, "item")
			if isResponsesCompactionItemType(item.Get("type").String()) {
				appendItem(item)
			}
		})
	}
	if len(items) == 0 {
		return nil, false
	}
	outputJSON, err := json.Marshal(items)
	if err != nil {
		return nil, false
	}
	return outputJSON, true
}

func isResponsesCompactionItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "compaction", "compaction_summary":
		return true
	default:
		return false
	}
}

func supplementCompactionItemFromSSE(c *gin.Context, finalResponse []byte, bodyText string) []byte {
	if !isOpenAIResponsesCompactPath(c) || len(gjson.GetBytes(finalResponse, "output").Array()) == 0 || responsesOutputHasCompactionItem(finalResponse) {
		return finalResponse
	}
	item, found := findRawCompactionItemFromSSE(bodyText)
	if !found {
		return finalResponse
	}
	patched, err := sjson.SetRawBytes(finalResponse, "output.-1", item)
	if err != nil {
		return finalResponse
	}
	return patched
}

func responsesOutputHasCompactionItem(response []byte) bool {
	for _, item := range gjson.GetBytes(response, "output").Array() {
		if isResponsesCompactionItemType(item.Get("type").String()) {
			return true
		}
	}
	return false
}

func findRawCompactionItemFromSSE(bodyText string) (json.RawMessage, bool) {
	var found json.RawMessage
	pick := func(eventType string) {
		forEachOpenAISSEDataPayload(bodyText, func(data []byte) {
			if found != nil || strings.TrimSpace(gjson.GetBytes(data, "type").String()) != eventType {
				return
			}
			item := gjson.GetBytes(data, "item")
			if item.IsObject() && isResponsesCompactionItemType(item.Get("type").String()) {
				found = json.RawMessage(item.Raw)
			}
		})
	}
	pick("response.output_item.done")
	if found == nil {
		pick("response.output_item.added")
	}
	return found, found != nil
}
