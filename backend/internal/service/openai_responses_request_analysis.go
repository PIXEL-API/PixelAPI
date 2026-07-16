package service

import (
	"errors"
	"strings"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var (
	ErrOpenAIResponsesRequestBodyEmpty       = errors.New("request body is empty")
	ErrOpenAIResponsesInvalidJSON            = errors.New("invalid json")
	ErrOpenAIResponsesInvalidModelFieldType  = errors.New("invalid model field type")
	ErrOpenAIResponsesInvalidStreamFieldType = errors.New("invalid stream field type")
)

type OpenAIResponsesRequestAnalysis struct {
	Body                         []byte
	Model                        string
	ModelExists                  bool
	Stream                       bool
	PromptCacheKey               string
	PreviousResponseID           string
	FunctionCallOutputValidation FunctionCallOutputValidation

	inputResult              gjson.Result
	instructionsResult       gjson.Result
	toolsResult              gjson.Result
	functionsResult          gjson.Result
	messagesResult           gjson.Result
	topLevelResultsReady     bool
	contentModerationInput   ContentModerationInput
	contentModerationReady   bool
	cyberPreflightInput      ContentModerationInput
	cyberPreflightInputReady bool
	contentSessionSeed       string
	contentSeedReady         bool
}

func AnalyzeOpenAIResponsesRequest(body []byte) (*OpenAIResponsesRequestAnalysis, error) {
	if len(body) == 0 {
		return nil, ErrOpenAIResponsesRequestBodyEmpty
	}
	if !gjson.ValidBytes(body) {
		return nil, ErrOpenAIResponsesInvalidJSON
	}

	jsonView := unsafe.String(unsafe.SliceData(body), len(body))
	root := gjson.Parse(jsonView)
	var modelResult, streamResult, promptCacheKeyResult, previousResponseIDResult gjson.Result
	var inputResult, instructionsResult, toolsResult, functionsResult, messagesResult gjson.Result
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			if !modelResult.Exists() {
				modelResult = value
			}
		case "stream":
			if !streamResult.Exists() {
				streamResult = value
			}
		case "prompt_cache_key":
			if !promptCacheKeyResult.Exists() {
				promptCacheKeyResult = value
			}
		case "previous_response_id":
			if !previousResponseIDResult.Exists() {
				previousResponseIDResult = value
			}
		case "input":
			if !inputResult.Exists() {
				inputResult = value
			}
		case "instructions":
			if !instructionsResult.Exists() {
				instructionsResult = value
			}
		case "tools":
			if !toolsResult.Exists() {
				toolsResult = value
			}
		case "functions":
			if !functionsResult.Exists() {
				functionsResult = value
			}
		case "messages":
			if !messagesResult.Exists() {
				messagesResult = value
			}
		}
		return true
	})
	if modelResult.Exists() && modelResult.Type != gjson.String {
		return nil, ErrOpenAIResponsesInvalidModelFieldType
	}
	if streamResult.Exists() && streamResult.Type != gjson.True && streamResult.Type != gjson.False {
		return nil, ErrOpenAIResponsesInvalidStreamFieldType
	}
	promptCacheKey := strings.TrimSpace(promptCacheKeyResult.String())
	previousResponseID := strings.TrimSpace(previousResponseIDResult.String())

	return &OpenAIResponsesRequestAnalysis{
		Body:                         body,
		Model:                        modelResult.String(),
		ModelExists:                  modelResult.Exists(),
		Stream:                       streamResult.Bool(),
		PromptCacheKey:               promptCacheKey,
		PreviousResponseID:           previousResponseID,
		FunctionCallOutputValidation: validateFunctionCallOutputContextFromResponsesInput(inputResult),
		inputResult:                  inputResult,
		instructionsResult:           instructionsResult,
		toolsResult:                  toolsResult,
		functionsResult:              functionsResult,
		messagesResult:               messagesResult,
		topLevelResultsReady:         true,
	}, nil
}

func AnalyzeOpenAIResponsesSessionRequest(body []byte) *OpenAIResponsesRequestAnalysis {
	return &OpenAIResponsesRequestAnalysis{
		Body:           body,
		PromptCacheKey: strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()),
	}
}

func (a *OpenAIResponsesRequestAnalysis) WithBodyAndModel(body []byte, model string) *OpenAIResponsesRequestAnalysis {
	if a == nil {
		return nil
	}
	next := *a
	next.Body = body
	next.Model = strings.TrimSpace(model)
	next.ModelExists = next.Model != ""
	next.contentSessionSeed = ""
	next.contentSeedReady = false
	if next.Model == "" {
		next.Model = a.Model
		next.ModelExists = a.ModelExists
	}
	return &next
}

func (a *OpenAIResponsesRequestAnalysis) ContentModerationInputCopy() ContentModerationInput {
	if a == nil {
		return ContentModerationInput{}
	}
	if !a.contentModerationReady {
		a.contentModerationInput = extractResponsesContentModerationInput(a.inputResult)
		a.contentModerationReady = true
	}
	return a.contentModerationInput.Clone()
}

func (a *OpenAIResponsesRequestAnalysis) CyberPreflightInputCopy() ContentModerationInput {
	if a == nil {
		return ContentModerationInput{}
	}
	if !a.cyberPreflightInputReady {
		a.cyberPreflightInput = extractOpenAIResponsesCyberPreflightInput(a.instructionsResult, a.inputResult)
		a.cyberPreflightInputReady = true
	}
	return a.cyberPreflightInput.Clone()
}

func (a *OpenAIResponsesRequestAnalysis) contentDerivedSessionSeed() string {
	if a == nil {
		return ""
	}
	if !a.contentSeedReady {
		if a.topLevelResultsReady {
			a.contentSessionSeed = deriveOpenAIContentSessionSeedFromResults(
				a.Model,
				a.toolsResult,
				a.functionsResult,
				a.instructionsResult,
				a.messagesResult,
				a.inputResult,
			)
		} else {
			a.contentSessionSeed = deriveOpenAIContentSessionSeed(a.Body)
		}
		a.contentSeedReady = true
	}
	return a.contentSessionSeed
}

func (s *OpenAIGatewayService) GenerateSessionHashFromAnalysis(c *gin.Context, analysis *OpenAIResponsesRequestAnalysis) string {
	if analysis == nil {
		return s.GenerateSessionHash(c, nil)
	}
	sessionID := explicitOpenAISessionIDFromAnalysis(c, analysis)
	if sessionID == "" {
		sessionID = analysis.contentDerivedSessionSeed()
	}
	if sessionID == "" {
		return ""
	}

	currentHash, legacyHash := deriveOpenAISessionHashes(sessionID)
	attachOpenAILegacySessionHashToGin(c, legacyHash)
	return currentHash
}

func explicitOpenAISessionIDFromAnalysis(c *gin.Context, analysis *OpenAIResponsesRequestAnalysis) string {
	if c == nil {
		return ""
	}
	sessionID := strings.TrimSpace(c.GetHeader("session_id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(c.GetHeader("conversation_id"))
	}
	if sessionID == "" && analysis != nil {
		sessionID = strings.TrimSpace(analysis.PromptCacheKey)
	}
	return sessionID
}

func validateFunctionCallOutputContextFromResponsesInput(input gjson.Result) FunctionCallOutputValidation {
	result := FunctionCallOutputValidation{}
	if !input.IsArray() {
		return result
	}

	callIDs := make(map[string]struct{})
	referenceIDs := make(map[string]struct{})
	input.ForEach(func(_, item gjson.Result) bool {
		itemType := item.Get("type").String()
		switch itemType {
		case "function_call_output":
			result.HasFunctionCallOutput = true
			callID := strings.TrimSpace(item.Get("call_id").String())
			if callID == "" {
				result.HasFunctionCallOutputMissingCallID = true
			} else {
				callIDs[callID] = struct{}{}
			}
		case "tool_call", "function_call":
			if strings.TrimSpace(item.Get("call_id").String()) != "" {
				result.HasToolCallContext = true
			}
		case "item_reference":
			if idValue := strings.TrimSpace(item.Get("id").String()); idValue != "" {
				referenceIDs[idValue] = struct{}{}
			}
		}
		return true
	})
	if result.HasToolCallContext {
		return FunctionCallOutputValidation{
			HasFunctionCallOutput: result.HasFunctionCallOutput,
			HasToolCallContext:    true,
		}
	}
	if !result.HasFunctionCallOutput {
		return result
	}
	if len(callIDs) == 0 || len(referenceIDs) == 0 {
		return result
	}
	for callID := range callIDs {
		if _, ok := referenceIDs[callID]; !ok {
			return result
		}
	}
	result.HasItemReferenceForAllCallIDs = true
	return result
}
