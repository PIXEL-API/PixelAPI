package service

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// countGrokNativeSearchCallsFromJSONBytes counts completed native search tool
// calls in a Responses-style JSON body (output array or nested response.output).
// Counts: web_search_call, x_search_call, tool_search_call, and function/custom
// calls named tool_search, web_search, or x_search.
func countGrokNativeSearchCallsFromJSONBytes(body []byte) int {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return 0
	}
	// Compatibility layers can retain top-level output and response.output at
	// the same time. Prefer the canonical nested response to avoid billing one
	// upstream search twice.
	if nested := gjson.GetBytes(body, "response.output"); nested.IsArray() {
		return countGrokNativeSearchCallsInOutputArray(nested)
	}
	return countGrokNativeSearchCallsInOutputArray(gjson.GetBytes(body, "output"))
}

func countGrokNativeSearchCallsFromSSEBody(body string) int {
	if strings.TrimSpace(body) == "" {
		return 0
	}
	seen := make(map[string]struct{})
	total := 0
	forEachOpenAISSEDataPayload(body, func(data []byte) {
		total += countGrokNativeSearchCallsInSSEDataDedup(data, seen)
	})
	return total
}

// countGrokNativeSearchCallsInSSEData counts search tool calls in one SSE
// payload without cross-event deduplication.
func countGrokNativeSearchCallsInSSEData(data []byte) int {
	n, _ := countGrokNativeSearchCallsInSSEDataWithKeys(data)
	return n
}

// countGrokNativeSearchCallsInSSEDataDedup increments only unseen calls.
// Callers must reuse seen for the full stream lifetime so item.done and the
// terminal response do not both charge the same invocation.
func countGrokNativeSearchCallsInSSEDataDedup(data []byte, seen map[string]struct{}) int {
	if seen == nil {
		return countGrokNativeSearchCallsInSSEData(data)
	}
	n, keys := countGrokNativeSearchCallsInSSEDataWithKeys(data)
	if n <= 0 {
		return 0
	}
	if len(keys) < n {
		keys = collectGrokNativeSearchCallKeys(data)
	}
	if len(keys) == 0 {
		// A billable-looking event without an attributable call must not be
		// guessed into a count. The shared billing layer separately fails closed
		// when a positive, attributable count has no configured price.
		return 0
	}
	added := 0
	local := make(map[string]struct{}, len(keys))
	isItemDone := strings.TrimSpace(gjson.GetBytes(data, "type").String()) == "response.output_item.done"
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, duplicate := local[key]; duplicate {
			continue
		}
		local[key] = struct{}{}
		if _, duplicate := seen[key]; duplicate {
			if !isItemDone || !strings.HasPrefix(key, "synth:") {
				continue
			}
			// Consecutive id-less item.done events are distinct completed calls.
			// Advance the synthetic ordinal; the terminal aggregate will reuse the
			// same ordered keys and therefore add no duplicate charge.
			separator := strings.LastIndexByte(key, ':')
			if separator < 0 {
				continue
			}
			base := key[:separator]
			for ordinal := 2; ; ordinal++ {
				candidate := base + ":" + strconv.Itoa(ordinal)
				if _, exists := seen[candidate]; !exists {
					key = candidate
					break
				}
			}
		}
		seen[key] = struct{}{}
		added++
	}
	return added
}

func collectGrokNativeSearchCallKeys(data []byte) []string {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return nil
	}
	switch strings.TrimSpace(gjson.GetBytes(data, "type").String()) {
	case "response.output_item.done", "response.completed", "response.done", "":
	default:
		return nil
	}

	var keys []string
	syntheticOrdinals := make(map[string]int)
	consider := func(item gjson.Result) {
		if !isGrokNativeSearchOutputItem(item) {
			return
		}
		key := firstNonEmpty(
			strings.TrimSpace(item.Get("call_id").String()),
			strings.TrimSpace(item.Get("id").String()),
			strings.TrimSpace(item.Get("item.call_id").String()),
			strings.TrimSpace(item.Get("item.id").String()),
		)
		if key == "" {
			base := "synth:" + strings.ToLower(strings.TrimSpace(item.Get("type").String())) +
				":" + strings.ToLower(strings.TrimSpace(item.Get("name").String()))
			syntheticOrdinals[base]++
			key = base + ":" + strconv.Itoa(syntheticOrdinals[base])
		}
		keys = append(keys, key)
	}
	if item := gjson.GetBytes(data, "item"); item.Exists() {
		consider(item)
	}
	gjson.GetBytes(data, "response.output").ForEach(func(_, item gjson.Result) bool {
		consider(item)
		return true
	})
	gjson.GetBytes(data, "output").ForEach(func(_, item gjson.Result) bool {
		consider(item)
		return true
	})
	if len(keys) == 0 && isGrokNativeSearchOutputItem(gjson.ParseBytes(data)) {
		consider(gjson.ParseBytes(data))
	}
	return keys
}

func countGrokNativeSearchCallsInSSEDataWithKeys(data []byte) (int, []string) {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return 0, nil
	}
	switch strings.TrimSpace(gjson.GetBytes(data, "type").String()) {
	case "response.output_item.done", "response.completed", "response.done", "":
	default:
		return 0, nil
	}

	var keys []string
	count := 0
	consider := func(item gjson.Result) {
		if !isGrokNativeSearchOutputItem(item) {
			return
		}
		count++
		key := firstNonEmpty(
			strings.TrimSpace(item.Get("call_id").String()),
			strings.TrimSpace(item.Get("id").String()),
			strings.TrimSpace(item.Get("item.call_id").String()),
			strings.TrimSpace(item.Get("item.id").String()),
		)
		if key != "" {
			keys = append(keys, key)
		}
	}
	if item := gjson.GetBytes(data, "item"); item.Exists() {
		consider(item)
	}
	gjson.GetBytes(data, "response.output").ForEach(func(_, item gjson.Result) bool {
		consider(item)
		return true
	})
	gjson.GetBytes(data, "output").ForEach(func(_, item gjson.Result) bool {
		consider(item)
		return true
	})
	if count == 0 && isGrokNativeSearchOutputItem(gjson.ParseBytes(data)) {
		consider(gjson.ParseBytes(data))
	}
	return count, keys
}

func countGrokNativeSearchCallsInOutputArray(output gjson.Result) int {
	if !output.IsArray() {
		return 0
	}
	count := 0
	output.ForEach(func(_, item gjson.Result) bool {
		if isGrokNativeSearchOutputItem(item) {
			count++
		}
		return true
	})
	return count
}

func isGrokNativeSearchOutputItem(item gjson.Result) bool {
	if !item.Exists() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.Get("type").String())) {
	case "web_search_call", "x_search_call", "tool_search_call":
		return true
	case "function_call", "custom_tool_call":
		name := strings.ToLower(strings.TrimSpace(item.Get("name").String()))
		return name == "web_search" || name == "x_search" || name == "tool_search"
	default:
		return false
	}
}
