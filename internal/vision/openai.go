package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// normalizeTime fixes invalid HH:MM values. In particular, 24:00 becomes 23:59.
func normalizeTime(t string) string {
	if t == "24:00" {
		return "23:59"
	}
	return t
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
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// doRequest executes an OpenAI API request with logging.
func (c *Client) doRequest(req *http.Request, caller string, model string) (*http.Response, error) {
	log.Printf("OPENAI API CALL: %s (model: %s)", caller, model)
	return c.httpClient.Do(req)
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

	currentYear := time.Now().Year()
	prompt := fmt.Sprintf(`Extract ALL church service schedule entries from this image. The schedule is dense and may contain 30+ entries — be extremely thorough and do not skip any.

STEP 1: First, scan the entire image top to bottom (and left column then right column if multi-column) and identify every date header (e.g., "Κυριακή 1 Μαρτίου", "Torsdag 26 mars"). List them mentally — you must not miss any date section.

STEP 2: For each date header, extract every service listed under it. A single date may have multiple services at different times. Times may appear right-aligned or at the end of a wrapped line — look carefully for them. Also include any annotations, notes, or special entries marked with "NOTERING", "OBS", "NOTE" or similar — these are additional events (often at other locations) and must be extracted as separate entries.

The image may be in any language (Greek, Swedish, etc.). Keep all text in its ORIGINAL language — do NOT translate.

Return a JSON object with these fields:
- language: the language of the schedule (e.g., "Swedish", "Greek", "English")
- entries: an array of services, each with:
  - date: in YYYY-MM-DD format (use year %d if not specified)
  - day_of_week: the day name in the ORIGINAL language
  - time: in HH:MM format (24-hour). Convert "π.μ." to AM and "μ.μ." to PM times in 24h format (e.g., 6:00 μ.μ. = 18:00)
  - service_name: the name of the service in the ORIGINAL language
  - occasion: optional, any special occasion or holiday mentioned, in the ORIGINAL language

Only include entries that have both a date/day and a time specified. Note that NOTERING/NOTE entries also have times — the time typically appears right-aligned at the end of the last line of wrapped text (e.g., after a closing parenthesis).
IMPORTANT: Double-check that you have not skipped any date sections or services. The output should cover the ENTIRE schedule from first date to last date. Count the number of date headers you found and verify none were skipped. Verify that no entry has time 00:00 unless it genuinely says midnight.
Return ONLY the JSON object, no other text.`, currentYear)

	reqBody := map[string]interface{}{
		"model": "gpt-4.1",
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

	resp, err := c.doRequest(req, "ExtractScheduleRaw", "gpt-4.1")
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

// ExtractScheduleFromText sends text to OpenAI's API and extracts church service schedule entries.
func (c *Client) ExtractScheduleFromText(ctx context.Context, text string) ([]ScheduleEntry, error) {
	today := time.Now().Format("January 2, 2006")
	prompt := fmt.Sprintf(`Extract church service schedule information from this text.
Return a JSON array of services with these fields:
- date: in YYYY-MM-DD format. IMPORTANT: Today is %s. Extract ALL events including past ones. If the text mentions a year that would place all events in the past, it is likely a typo; use the current year instead. If no year is specified, use 2026.
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

	resp, err := c.doRequest(req, "ExtractScheduleFromText", "gpt-4o")
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

	for i := range entries {
		entries[i].Time = normalizeTime(entries[i].Time)
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

	resp, err := c.doRequest(req, "TranslateScheduleEntries", "gpt-4o-mini")
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

// GenerateTitles sends a list of service names to gpt-4o-mini and returns
// a map from service_name to a short 1-2 word title.
func (c *Client) GenerateTitles(ctx context.Context, serviceNames []string) (map[string]string, error) {
	if len(serviceNames) == 0 {
		return map[string]string{}, nil
	}

	namesJSON, err := json.Marshal(serviceNames)
	if err != nil {
		return nil, fmt.Errorf("marshaling service names: %w", err)
	}

	prompt := fmt.Sprintf(`You are given a JSON array of Orthodox church service names (in Swedish). For each service name, generate a short title of 1-2 words that captures the essence of the service. The title should be in Swedish.

Examples:
- "Gudomlig liturgi" → "Gudomlig Liturgi"
- "Helig Liturgi" → "Gudomlig Liturgi"
- "Liturgi" → "Gudomlig Liturgi"
- "Ärkeprästerlig Gudomlig Liturgi, med Hans Eminens Ärkebiskop Cleopas av Sverige" → "Gudomlig Liturgi"
- "Akathist till Guds moder - Andra hälsningen, med Hans Eminens Ärkebiskop Cleopas av Sverige" → "Akathist"
- "Stora bönetimmarna och vesper med basiliusliturgi" → "Bönetimmar"
- "Morgongudstjänst (Orthros/Matins)" → "Orthros"
- "Stora kompletoriet med den heliga Andreasakanonen" → "Kompletoriet"
- "Vesper" → "Vesper"
- "Trefaldighetsafton" → "Trefaldighetsafton"
- "Föreläsning för katekumener, med Hans Eminens Ärkebiskop Cleopas av Sverige" → "Katekesundervisning"
- "Katekes" → "Katekesundervisning"

IMPORTANT: Any service that is a form of Divine Liturgy (Gudomlig liturgi, Helig Liturgi, Liturgi, Ärkeprästerlig liturgi, etc.) must get the title "Gudomlig Liturgi".
IMPORTANT: Any service related to catechism or catechumens (katekumener, katekes, katekisundervisning, etc.) must get the title "Katekesundervisning".

Return a JSON object mapping each input service name (exactly as given) to its short title.

Input:
%s

Return ONLY the JSON object, no other text.`, string(namesJSON))

	reqBody := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": prompt,
			},
		},
		"max_tokens": 4096,
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

	resp, err := c.doRequest(req, "GenerateTitles", "gpt-4o-mini")
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

	var titles map[string]string
	if err := json.Unmarshal([]byte(content), &titles); err != nil {
		return nil, fmt.Errorf("parsing titles: %w (content: %s)", err, content)
	}

	return titles, nil
}

// EventInfo holds the relevant fields for determining an event's language.
type EventInfo struct {
	ServiceName string  `json:"service_name"`
	Occasion    *string `json:"occasion,omitempty"`
	Notes       *string `json:"notes,omitempty"`
}

// ParseEventLanguages examines event information and returns a map indicating which
// events explicitly specify a language. The map is keyed by array index (as a string).
// Returns nil values for events without an explicit language.
func (c *Client) ParseEventLanguages(ctx context.Context, events []EventInfo) (map[int]*string, error) {
	if len(events) == 0 {
		return map[int]*string{}, nil
	}

	const batchSize = 30
	result := make(map[int]*string, len(events))

	for start := 0; start < len(events); start += batchSize {
		end := start + batchSize
		if end > len(events) {
			end = len(events)
		}
		batch := events[start:end]

		batchResult, err := c.parseEventLanguagesBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d: %w", start, end, err)
		}

		for i, lang := range batchResult {
			result[start+i] = lang
		}
	}

	return result, nil
}

func (c *Client) parseEventLanguagesBatch(ctx context.Context, events []EventInfo) (map[int]*string, error) {
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return nil, fmt.Errorf("marshaling events: %w", err)
	}

	prompt := fmt.Sprintf(`You are given a JSON array of Orthodox church event objects, each with "service_name" and optionally "occasion" and "notes" fields (in Swedish). For each event, determine if it explicitly specifies the language of the event.

Look for phrases like "på svenska", "på engelska", "på finska", "på grekiska", "på arabiska", "på kyrkoslaviska", "på rumänska", "på serbiska" etc. The language mention may appear in the service_name, occasion, or notes fields.

For each event:
- If any field explicitly mentions a language, return that language name in Swedish (e.g., "Svenska", "Engelska", "Finska", "Grekiska")
- If no field explicitly mentions a language, return null

Examples:
- {"service_name": "Basileosliturgi på svenska"} → "Svenska"
- {"service_name": "Föreläsning om fastan", "notes": "Hålls på engelska"} → "Engelska"
- {"service_name": "Gudomlig liturgi"} → null
- {"service_name": "Vesper", "occasion": "Pingstafton"} → null
- {"service_name": "Liturgi på finska"} → "Finska"

Return a JSON array with EXACTLY %d elements (same length and order as input) where each element is a language string or null.

Input:
%s

Return ONLY the JSON array, no other text.`, len(events), string(eventsJSON))

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": prompt,
			},
		},
		"max_tokens": 4096,
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

	resp, err := c.doRequest(req, "parseEventLanguagesBatch", "gpt-4o")
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

	var raw []*string
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("parsing event languages: %w (content: %s)", err, content)
	}

	if len(raw) != len(events) {
		return nil, fmt.Errorf("expected %d results, got %d", len(events), len(raw))
	}

	result := make(map[int]*string, len(raw))
	for i, lang := range raw {
		result[i] = lang
	}

	return result, nil
}

// TimeEntry represents a date+time pair to be parsed into structured timestamps.
type TimeEntry struct {
	Date string `json:"date"` // "YYYY-MM-DD"
	Time string `json:"time"` // e.g. "18:00", "18:00 - 20:00", "18:00 - ca 20:30"
}

// ParsedTime holds the parsed start and optional end time.
type ParsedTime struct {
	Start time.Time  `json:"start"`
	End   *time.Time `json:"end,omitempty"`
}

// ParseTimes sends unique (date, time) pairs to gpt-4o-mini and returns structured
// timestamps in Europe/Stockholm timezone. The AI handles range parsing, "ca" prefix
// stripping, midnight crossing, and various formats.
func (c *Client) ParseTimes(ctx context.Context, entries []TimeEntry) (map[string]ParsedTime, error) {
	if len(entries) == 0 {
		return map[string]ParsedTime{}, nil
	}

	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshaling time entries: %w", err)
	}

	prompt := fmt.Sprintf(`You are given a JSON array of objects with "date" (YYYY-MM-DD) and "time" (free-form string) fields from a church service schedule.

For each entry, parse the time string and combine it with the date to produce start and end timestamps in Europe/Stockholm timezone (UTC+1 in winter, UTC+2 in summer).

Rules:
- Time strings may be a single time like "18:00" or a range like "18:00 - 20:00" or "18:00 - ca 20:30"
- Strip prefixes like "ca", "ca.", "kl", "kl." from times
- If only a start time is given, set end to null
- If a range is given, parse both start and end
- Handle midnight crossing: if end time is earlier than start time, it means the next day
- Output timestamps in RFC3339 format with the correct Europe/Stockholm UTC offset

Input:
%s

Return a JSON array (same order as input) of objects with:
- "start": RFC3339 timestamp string
- "end": RFC3339 timestamp string or null

Return ONLY the JSON array, no other text.`, string(entriesJSON))

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
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openaiAPIURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.doRequest(req, "ParseTimes", "gpt-4o-mini")
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

	var results []struct {
		Start string  `json:"start"`
		End   *string `json:"end"`
	}
	if err := json.Unmarshal([]byte(content), &results); err != nil {
		return nil, fmt.Errorf("parsing time results: %w (content: %s)", err, content)
	}

	if len(results) != len(entries) {
		return nil, fmt.Errorf("expected %d results, got %d", len(entries), len(results))
	}

	parsed := make(map[string]ParsedTime, len(entries))
	for i, entry := range entries {
		r := results[i]
		start, err := time.Parse(time.RFC3339, r.Start)
		if err != nil {
			log.Printf("WARNING: skipping bad start time for %s %s: %v", entry.Date, entry.Time, err)
			continue
		}
		pt := ParsedTime{Start: start}
		if r.End != nil {
			end, err := time.Parse(time.RFC3339, *r.End)
			if err != nil {
				log.Printf("WARNING: ignoring bad end time for %s %s: %v", entry.Date, entry.Time, err)
			} else {
				pt.End = &end
			}
		}
		key := entry.Date + "|" + entry.Time
		parsed[key] = pt
	}

	return parsed, nil
}

// CampEvent represents a single event extracted from a camp/event website.
type CampEvent struct {
	Date        string `json:"date"`         // YYYY-MM-DD
	DayOfWeek   string `json:"day_of_week"`  // Swedish day name
	ServiceName string `json:"service_name"` // Event description in Swedish
	Notes       string `json:"notes,omitempty"`
}

// ExtractCampEvents sends webpage text to OpenAI and extracts camp/event information.
// Returns individual day events for multi-day camps and reminder events for deadlines.
func (c *Client) ExtractCampEvents(ctx context.Context, text string) ([]CampEvent, error) {
	today := time.Now().Format("January 2, 2006")
	prompt := fmt.Sprintf(`Extract event information from this webpage text about an Orthodox summer camp.

Today is %s.

Generate the following events:
1. For the camp itself: create ONE event per day the camp runs (e.g., a Monday-Thursday camp = 4 events). Each day's service_name should be "Ortodoxt sommarlager" and notes should include the location and any relevant info (e.g., "Dag 1 av 4. Sjöbonäs lägergård, Kinnarumma").
2. For the registration deadline: create ONE event on the deadline date with service_name "Sista anmälningsdag: Ortodoxt sommarlager" and notes with registration details (price, link, etc).

Return a JSON array with these fields:
- date: YYYY-MM-DD format
- day_of_week: Swedish day name (e.g., "Måndag", "Tisdag")
- service_name: event description in Swedish
- notes: additional details

Return ONLY the JSON array, no other text.

Webpage text:
`, today) + text

	reqBody := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": prompt,
			},
		},
		"max_tokens": 4096,
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

	resp, err := c.doRequest(req, "ExtractCampEvents", "gpt-4o-mini")
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

	var events []CampEvent
	if err := json.Unmarshal([]byte(content), &events); err != nil {
		return nil, fmt.Errorf("parsing camp events: %w (content: %s)", err, content)
	}

	return events, nil
}

