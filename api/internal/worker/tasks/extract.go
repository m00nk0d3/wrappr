package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

const (
	// groqChatURL is the Groq OpenAI-compatible chat completions endpoint.
	groqChatURL = "https://api.groq.com/openai/v1/chat/completions"

	// groqLLMModel is the primary model used for structured extraction.
	groqLLMModel = "llama-3.3-70b-versatile"

	// geminiBaseURL is the Gemini generateContent endpoint. The ?key= query
	// param is appended at call time with the API key.
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent"

	// geminiModel is used for logging / ai_model_used DB field.
	geminiModel = "gemini-1.5-flash"

	// maxTags is the maximum number of job tags stored per job.
	maxTags = 5
)

// errGroqRateLimited is returned by callGroqLLM when Groq responds with HTTP 429.
var errGroqRateLimited = errors.New("groq: rate limited (429)")

// llmExtractionResult mirrors the JSON schema the LLM must return.
// All fields are nullable so partial extraction is handled gracefully.
type llmExtractionResult struct {
	Summary             string              `json:"summary"`
	WorkPerformed       string              `json:"work_performed"`
	MaterialsUsed       []llmMaterial       `json:"materials_used"`
	IssuesFound         []string            `json:"issues_found"`
	Recommendations     []llmRecommendation `json:"recommendations"`
	FollowUpRequired    bool                `json:"follow_up_required"`
	FollowUpNotes       string              `json:"follow_up_notes"`
	LaborHoursEstimated *float64            `json:"labor_hours_estimated"`
	JobTags             []string            `json:"job_tags"`
	ClientSentiment     string              `json:"client_sentiment"`
	WarrantyNotes       string              `json:"warranty_notes"`
	SafetyConcerns      []string            `json:"safety_concerns"`
	JobCategory         string              `json:"job_category"`
}

type llmMaterial struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type llmRecommendation struct {
	Description        string `json:"description"`
	Urgency            string `json:"urgency"`
	EstimatedCostRange string `json:"estimated_cost_range"`
}

// Groq OpenAI-compatible Chat Completions request / response.

type groqChatRequest struct {
	Model          string             `json:"model"`
	ResponseFormat groqResponseFormat `json:"response_format"`
	Temperature    float64            `json:"temperature"`
	Messages       []groqMessage      `json:"messages"`
}

type groqResponseFormat struct {
	Type string `json:"type"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqChatResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
}

// Gemini generateContent request / response.

type geminiRequest struct {
	SystemInstruction geminiContent   `json:"systemInstruction"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	ResponseMimeType string `json:"responseMimeType"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// controlledTagVocabulary is the fixed set of tags the LLM must choose from.
// Controlled vocabulary enables dashboard filtering and analytics (Decision 4 in issue #11).
var controlledTagVocabulary = strings.Join([]string{
	// Electrical
	"panel_upgrade", "circuit_breaker", "wiring_repair", "outlet_install",
	"lighting", "grounding", "safety_hazard",
	// Plumbing
	"leak_repair", "drain_clearing", "water_heater", "pipe_replacement",
	"valve_service", "toilet_repair", "faucet_repair",
	// HVAC
	"ac_service", "furnace_repair", "filter_change", "thermostat_install",
	"duct_cleaning", "refrigerant",
	// Cleaning
	"deep_clean", "post_construction_clean", "carpet_cleaning",
	"pressure_washing", "window_cleaning",
	// Landscaping
	"lawn_maintenance", "tree_service", "irrigation", "mulching", "hedge_trim",
	// General / cross-trade
	"new_installation", "maintenance", "repair", "inspection", "emergency",
	"warranty_repair", "follow_up_required", "safety_concern",
	"equipment_replaced", "permit_needed", "quote_provided",
	"completed_successfully", "customer_complaint", "repeat_visit",
}, ", ")

// buildExtractionPrompt returns the system prompt and user message for LLM extraction.
// reportLanguage is a BCP 47 language tag (e.g. "en", "pt-BR", "ar"); defaults to "en".
func buildExtractionPrompt(transcript, reportLanguage string) (system, user string) {
	if reportLanguage == "" {
		reportLanguage = "en"
	}
	system = fmt.Sprintf(`You are a field service report writer. Extract structured information from a technician's voice transcript.

Output all text fields in language: %s.
If the language uses right-to-left script (e.g. Arabic), use that script.
Return ONLY valid JSON — no explanation, no markdown, no code fences.
Use null for any field that cannot be determined from the transcript.

Constraints:
- job_category must be one of: electrical, plumbing, hvac, cleaning, landscaping, general, other
- client_sentiment must be one of: positive, neutral, negative, unavailable
- recommendation urgency must be one of: immediate, within_30_days, when_convenient
- job_tags must be lowercase, max 5 tags, chosen strictly from: %s
- summary must be max 120 characters
- labor_hours_estimated must be a number (e.g. 2.5) or null

JSON schema to return:
{
  "summary": "string or null",
  "work_performed": "string or null",
  "materials_used": [{"name": "string", "quantity": "string or null", "unit": "string or null"}],
  "issues_found": ["string"],
  "recommendations": [{"description": "string", "urgency": "string", "estimated_cost_range": "string or null"}],
  "follow_up_required": true,
  "follow_up_notes": "string or null",
  "labor_hours_estimated": 2.5,
  "job_tags": ["string"],
  "client_sentiment": "string or null",
  "warranty_notes": "string or null",
  "safety_concerns": ["string"],
  "job_category": "string or null"
}`, reportLanguage, controlledTagVocabulary)

	user = "Transcript:\n" + transcript
	return
}

// extractJobData calls Groq LLaMA 3.3 70B to extract structured data from transcript.
// If Groq returns HTTP 429 and a Gemini key is configured, it transparently falls back
// to Gemini 1.5 Flash. Returns the parsed result and the model name that was used.
func (h *ProcessJobHandler) extractJobData(ctx context.Context, transcript, reportLanguage string) (llmExtractionResult, string, error) {
	systemPrompt, userMsg := buildExtractionPrompt(transcript, reportLanguage)

	jsonText, err := h.callGroqLLM(ctx, systemPrompt, userMsg)
	modelUsed := groqLLMModel

	if errors.Is(err, errGroqRateLimited) {
		if h.geminiKey == "" {
			return llmExtractionResult{}, "", fmt.Errorf("groq rate limited and GEMINI_API_KEY is not configured: %w", err)
		}
		log.Printf("extract: groq rate limited — falling back to Gemini 1.5 Flash")
		jsonText, err = h.callGeminiLLM(ctx, systemPrompt, userMsg)
		modelUsed = geminiModel
	}

	if err != nil {
		return llmExtractionResult{}, "", fmt.Errorf("LLM call failed: %w", err)
	}

	var result llmExtractionResult
	if parseErr := json.Unmarshal([]byte(jsonText), &result); parseErr != nil {
		return llmExtractionResult{}, "", fmt.Errorf("parse LLM JSON: %w (raw: %.200s)", parseErr, jsonText)
	}

	return result, modelUsed, nil
}

// callGroqLLM sends a chat completion request to Groq with JSON mode enabled.
// Returns errGroqRateLimited if Groq responds with HTTP 429.
func (h *ProcessJobHandler) callGroqLLM(ctx context.Context, systemPrompt, userContent string) (string, error) {
	reqBody := groqChatRequest{
		Model:          groqLLMModel,
		ResponseFormat: groqResponseFormat{Type: "json_object"},
		Temperature:    0.1,
		Messages: []groqMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal groq request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.llmChatURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create groq request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.groqKey)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do groq request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read groq response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errGroqRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("groq returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp groqChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal groq response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("groq returned no choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// callGeminiLLM sends a generateContent request to Gemini 1.5 Flash with JSON output mode.
func (h *ProcessJobHandler) callGeminiLLM(ctx context.Context, systemPrompt, userContent string) (string, error) {
	reqBody := geminiRequest{
		SystemInstruction: geminiContent{Parts: []geminiPart{{Text: systemPrompt}}},
		Contents:          []geminiContent{{Parts: []geminiPart{{Text: userContent}}}},
		GenerationConfig:  geminiGenConfig{ResponseMimeType: "application/json"},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal gemini request: %w", err)
	}

	url := h.geminiChatURL + "?key=" + h.geminiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read gemini response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBytes, &gemResp); err != nil {
		return "", fmt.Errorf("unmarshal gemini response: %w", err)
	}
	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no content")
	}

	return gemResp.Candidates[0].Content.Parts[0].Text, nil
}

// sanitizeSentiment coerces client_sentiment to a known DB-safe value.
func sanitizeSentiment(s string) string {
	switch strings.ToLower(s) {
	case "positive", "neutral", "negative", "unavailable":
		return strings.ToLower(s)
	default:
		return "unavailable"
	}
}

// sanitizeCategory coerces job_category to a known DB-safe value.
func sanitizeCategory(s string) string {
	switch strings.ToLower(s) {
	case "electrical", "plumbing", "hvac", "cleaning", "landscaping", "general", "other":
		return strings.ToLower(s)
	default:
		return "other"
	}
}

// sanitizeUrgency coerces recommendation urgency to a DB-safe value.
// The job_recommendations table has a CHECK constraint on this column.
func sanitizeUrgency(s string) string {
	switch strings.ToLower(s) {
	case "immediate", "within_30_days", "when_convenient":
		return strings.ToLower(s)
	default:
		return "when_convenient"
	}
}

// normalizeTags lowercases, deduplicates, and truncates to max tags.
func normalizeTags(tags []string, max int) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, max)
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		result = append(result, t)
		if len(result) >= max {
			break
		}
	}
	return result
}
