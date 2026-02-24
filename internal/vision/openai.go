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

// ExtractSchedule sends an image to OpenAI's vision API and extracts church service schedule entries.
func (c *Client) ExtractSchedule(ctx context.Context, imageData []byte) ([]ScheduleEntry, error) {
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	mediaType := "image/jpeg"
	if len(imageData) > 8 && string(imageData[0:8]) == "\x89PNG\r\n\x1a\n" {
		mediaType = "image/png"
	}

	prompt := `Extract church service schedule information from this image.
Return a JSON array of services with these fields:
- date: in YYYY-MM-DD format (use year 2026 if not specified)
- day_of_week: the day name in Swedish (e.g., "Måndag", "Söndag")
- time: in HH:MM format (24-hour)
- service_name: the name of the service in Swedish
- occasion: optional, any special occasion or holiday mentioned

Only include entries that have both a date/day and a time specified.
Return ONLY the JSON array, no other text.`

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
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:%s;base64,%s", mediaType, imageBase64),
						},
					},
				},
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
