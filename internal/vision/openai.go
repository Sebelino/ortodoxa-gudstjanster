package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const openaiAPIURL = "https://api.openai.com/v1/chat/completions"

// ScheduleEntry represents a single church service extracted from an image.
type ScheduleEntry struct {
	Date        string `json:"date"`
	DayOfWeek   string `json:"day_of_week"`
	Time        string `json:"time"`
	ServiceName string `json:"service_name"`
	Occasion    string `json:"occasion,omitempty"`
}

// RawScheduleResult holds the raw OCR output from an image in its original language.
type RawScheduleResult struct {
	Language string             `json:"language"` // e.g. "Swedish", "Greek"
	Entries  []RawScheduleEntry `json:"entries"`
}

// RawScheduleEntry is a single service entry in its original language.
type RawScheduleEntry struct {
	Date        string `json:"date"`
	DayOfWeek   string `json:"day_of_week"`
	Time        string `json:"time"`
	ServiceName string `json:"service_name"`
	Occasion    string `json:"occasion,omitempty"`
}

// Client is an OpenAI Vision API client.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new OpenAI Vision client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// ExtractScheduleRaw sends an image to OpenAI's vision API and extracts church service
// schedule entries in their original language. Returns the structured result and the
// raw API response content for diagnostics.
func (c *Client) ExtractScheduleRaw(ctx context.Context, imageData []byte) (*RawScheduleResult, string, error) {
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	mediaType := "image/jpeg"
	if len(imageData) > 8 && string(imageData[0:8]) == "\x89PNG\r\n\x1a\n" {
		mediaType = "image/png"
	}

	prompt := `Extract ALL church service schedule entries from this image. The schedule is dense and may contain 30+ entries — be extremely thorough and do not skip any.

STEP 1: First, scan the entire image top to bottom (and left column then right column if multi-column) and identify every date header (e.g., "Κυριακή 1 Μαρτίου", "Torsdag 26 mars"). List them mentally — you must not miss any date section.

STEP 2: For each date header, extract every service listed under it. A single date may have multiple services at different times. Times may appear right-aligned or at the end of a wrapped line — look carefully for them. Also include any annotations, notes, or special entries marked with "NOTERING", "OBS", "NOTE" or similar — these are additional events (often at other locations) and must be extracted as separate entries.

The image may be in any language (Greek, Swedish, etc.). Keep all text in its ORIGINAL language — do NOT translate.

Return a JSON object with these fields:
- language: the language of the schedule (e.g., "Swedish", "Greek", "English")
- entries: an array of services, each with:
  - date: in YYYY-MM-DD format (use year 2026 if not specified)
  - day_of_week: the day name in the ORIGINAL language
  - time: in HH:MM format (24-hour). Convert "π.μ." to AM and "μ.μ." to PM times in 24h format (e.g., 6:00 μ.μ. = 18:00)
  - service_name: the name of the service in the ORIGINAL language
  - occasion: optional, any special occasion or holiday mentioned, in the ORIGINAL language

Only include entries that have both a date/day and a time specified.
IMPORTANT: Double-check that you have not skipped any date sections or services. The output should cover the ENTIRE schedule from first date to last date. Count the number of date headers you found and verify none were skipped.
Return ONLY the JSON object, no other text.`

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url":    fmt.Sprintf("data:%s;base64,%s", mediaType, imageBase64),
							"detail": "high",
						},
					},
				},
			},
		},
		"max_tokens": 16384,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openaiAPIURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, "", fmt.Errorf("parsing API response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, "", fmt.Errorf("no response from API")
	}

	content := apiResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result RawScheduleResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, content, fmt.Errorf("parsing raw schedule result: %w (content: %s)", err, content)
	}

	return &result, content, nil
}

// VerifyScheduleExtraction sends the image again along with previously extracted entries
// and asks the model to find any missed events. Returns additional entries not in the original.
func (c *Client) VerifyScheduleExtraction(ctx context.Context, imageData []byte, existing *RawScheduleResult) ([]RawScheduleEntry, string, error) {
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	mediaType := "image/jpeg"
	if len(imageData) > 8 && string(imageData[0:8]) == "\x89PNG\r\n\x1a\n" {
		mediaType = "image/png"
	}

	existingJSON, err := json.Marshal(existing)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling existing entries: %w", err)
	}

	prompt := fmt.Sprintf(`I already extracted the following church service schedule entries from this image:

%s

Please look at the image VERY carefully and find any events that are MISSING from the list above. Check every date section in the image (both columns if the schedule has two columns). Pay special attention to:
- Events where the time appears at the end of a wrapped line
- Events in dense sections that may have been overlooked
- Lecture/catechesis events, special services, and non-liturgical events

If you find missing events, return a JSON object with:
- language: same as above ("%s")
- entries: array of ONLY the missing events, each with:
  - date: in YYYY-MM-DD format (use year 2026 if not specified)
  - day_of_week: the day name in the ORIGINAL language
  - time: in HH:MM format (24-hour)
  - service_name: the name of the service in the ORIGINAL language
  - occasion: optional, any special occasion in the ORIGINAL language

If no events are missing, return: {"language": "%s", "entries": []}
Return ONLY the JSON object, no other text.`, string(existingJSON), existing.Language, existing.Language)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url":    fmt.Sprintf("data:%s;base64,%s", mediaType, imageBase64),
							"detail": "high",
						},
					},
				},
			},
		},
		"max_tokens": 16384,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openaiAPIURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, "", fmt.Errorf("parsing API response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, "", fmt.Errorf("no response from API")
	}

	content := apiResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result RawScheduleResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, content, fmt.Errorf("parsing verification result: %w (content: %s)", err, content)
	}

	return result.Entries, content, nil
}

// ExtractScheduleFromText sends text to OpenAI's API and extracts church service schedule entries.
func (c *Client) ExtractScheduleFromText(ctx context.Context, text string) ([]ScheduleEntry, error) {
	today := time.Now().Format("January 2, 2006")
	prompt := fmt.Sprintf(`Extract church service schedule information from this text.
Return a JSON array of services with these fields:
- date: in YYYY-MM-DD format. IMPORTANT: Today is %s. Church schedules are always for the current or upcoming period — never for the past. If the text mentions a year that would place all events in the past, it is likely a typo; use the current year instead. If no year is specified, use 2026.
- day_of_week: the day name in Swedish (e.g., "Måndag", "Söndag")
- time: in HH:MM format (24-hour)
- service_name: the name of the service in Swedish
- occasion: optional, any special occasion or holiday mentioned

Only include entries that have both a date/day and a time specified.
Return ONLY the JSON array, no other text.

Text to parse:
`, today) + text

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": prompt,
			},
		},
		"max_tokens": 16384,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openaiAPIURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing API response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from API")
	}

	content := apiResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var entries []ScheduleEntry
	if err := json.Unmarshal([]byte(content), &entries); err != nil {
		return nil, fmt.Errorf("parsing schedule entries: %w (content: %s)", err, content)
	}

	return entries, nil
}

// TranslateScheduleEntries translates raw schedule entries to Swedish using a text-only
// OpenAI call. Returns the translated entries and the raw API response content.
func (c *Client) TranslateScheduleEntries(ctx context.Context, entries []RawScheduleEntry) ([]ScheduleEntry, string, error) {
	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling entries: %w", err)
	}

	today := time.Now().Format("January 2, 2006")
	prompt := fmt.Sprintf(`Translate these church service schedule entries to Swedish.
Today is %s.

Input JSON:
%s

Return a JSON array of services with these fields:
- date: in YYYY-MM-DD format (keep the same dates)
- day_of_week: the day name in Swedish (e.g., "Måndag", "Söndag")
- time: in HH:MM format (24-hour, keep the same times)
- service_name: the name of the service translated to Swedish (e.g., "Θεία Λειτουργία" → "Gudomlig liturgi")
- occasion: optional, any special occasion or holiday, translated to Swedish

Return ONLY the JSON array, no other text.`, today, string(entriesJSON))

	reqBody := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": prompt,
			},
		},
		"max_tokens": 16384,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openaiAPIURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, "", fmt.Errorf("parsing API response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, "", fmt.Errorf("no response from API")
	}

	content := apiResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var translated []ScheduleEntry
	if err := json.Unmarshal([]byte(content), &translated); err != nil {
		return nil, content, fmt.Errorf("parsing translated entries: %w (content: %s)", err, content)
	}

	return translated, content, nil
}

// MergeScheduleEntries merges multiple OCR results (from the same schedule in different
// languages) into a single Swedish schedule. Includes all events from all versions,
// uses Swedish names and times when available.
func (c *Client) MergeScheduleEntries(ctx context.Context, schedules []RawScheduleResult) ([]ScheduleEntry, string, error) {
	schedulesJSON, err := json.Marshal(schedules)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling schedules: %w", err)
	}

	today := time.Now().Format("January 2, 2006")
	prompt := fmt.Sprintf(`You are given church service schedule entries extracted via OCR from multiple images of the same monthly schedule in different languages. OCR may introduce errors in dates or times.

Merge them into a single, complete Swedish schedule following these rules:
1. The Swedish version is the AUTHORITATIVE source for dates, times, and service names. Non-Swedish versions are supplementary and may contain OCR errors.
2. For each date that exists in the Swedish version, use ONLY the Swedish events for that date. Do NOT add extra events from non-Swedish versions to dates that already exist in the Swedish schedule.
3. Match events across versions by date and approximate time (within 30 minutes = same event). Use the SWEDISH time when they differ.
4. If a non-Swedish version has an event on a date that does NOT exist in the Swedish version at all, include it (translated to Swedish). But first check if a similar event exists on a nearby date in the Swedish version — if so, it is likely an OCR date error and should NOT be added as a separate event.
5. day_of_week must be in Swedish (e.g., "Måndag", "Söndag").
6. Use Swedish service names. Translate non-Swedish names only when adding events that have no Swedish counterpart.

Today is %s.

Input schedules (each with language and entries):
%s

Return a JSON array of merged services with these fields:
- date: in YYYY-MM-DD format
- day_of_week: in Swedish
- time: in HH:MM format (24-hour)
- service_name: in Swedish
- occasion: optional, in Swedish

Return ONLY the JSON array, no other text.`, today, string(schedulesJSON))

	reqBody := map[string]interface{}{
		"model": "gpt-4.1",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": prompt,
			},
		},
		"max_tokens": 16384,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openaiAPIURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, "", fmt.Errorf("parsing API response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, "", fmt.Errorf("no response from API")
	}

	content := apiResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var merged []ScheduleEntry
	if err := json.Unmarshal([]byte(content), &merged); err != nil {
		return nil, content, fmt.Errorf("parsing merged entries: %w (content: %s)", err, content)
	}

	return merged, content, nil
}
