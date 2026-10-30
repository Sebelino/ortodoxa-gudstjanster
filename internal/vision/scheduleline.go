package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// LineService is one service found in a free-text schedule line.
type LineService struct {
	Time      string `json:"time"`       // HH:MM, 24-hour
	Name      string `json:"name"`       // verbatim fragment of the line describing the service
	IsOrthros bool   `json:"is_orthros"` // the service itself is an Orthros (matins)
}

// LineDay is one calendar day mentioned in a schedule line. A day with no
// services is an announcement that nothing takes place (e.g. "Ingen Liturgi").
type LineDay struct {
	Day      int           `json:"day"`
	Month    int           `json:"month"`
	Services []LineService `json:"services"`
}

// LineSchedule is the structured reading of one free-text schedule line.
type LineSchedule struct {
	Days []LineDay `json:"days"`
}

const scheduleLinePrompt = `You read one line of free text from a Swedish Orthodox parish's website schedule. The wording is inconsistent: months may be abbreviated or misspelled ("okt", "ok"), colons and weekdays may be missing, and a line may mention several services or even several dates.

Return JSON: {"days":[{"day":<1-31>,"month":<1-12>,"services":[{"time":"HH:MM","name":"...","is_orthros":false}]}]}

Rules:
- One entry in "days" per calendar date the line announces services (or cancellations) for. Do not invent dates, and do not include a date that is only a fallback or reference inside another service's description unless it has its own service time.
- One entry in "services" per distinct service with its own start time. When a line has both an Orthros and a Divine Liturgy, return both. Confession (bikt) with its own time is a separate service too.
- "time" is the start time in 24-hour HH:MM ("kl 10" -> "10:00", "kl:9.30" -> "09:30"). For a range such as 13:00-15:00 use the start. The time must actually appear in the line.
- "name" is a contiguous fragment copied character-for-character from the line (same spelling, typos and punctuation), never translated or corrected. If the line describes a single service, use the whole text after the date/weekday. If it describes several, use for each the shortest fragment that includes that service's time; the main service may keep the whole text after the date so its details (place, coffee, booking info) are kept.
- "is_orthros" is true only for the Orthros service itself ("Orthros", "Orhros", "Matins"), not for a liturgy that merely mentions an Orthros starting earlier.
- If the line announces that a service does not take place ("Ingen Liturgi"), return the day with "services": [].
- If the line is not a schedule entry (a quote, a saint's name, general information), return {"days":[]}.
- Return ONLY the JSON object.

Line:
`

// ParseScheduleLine reads one free-text schedule line into structured days and
// services. The output is not validated against the line; callers should do that.
func (c *Client) ParseScheduleLine(ctx context.Context, line string) (*LineSchedule, error) {
	reqBody := map[string]interface{}{
		"model":           "gpt-4o-mini",
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": scheduleLinePrompt + line},
		},
		"max_tokens": 2048,
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

	resp, err := c.doRequest(req, "ParseScheduleLine", "gpt-4o-mini")
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

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)
	var result LineSchedule
	if err := parseLeadingJSON(content, &result); err != nil {
		return nil, fmt.Errorf("parsing schedule line: %w (content: %s)", err, content)
	}
	for i := range result.Days {
		for j := range result.Days[i].Services {
			result.Days[i].Services[j].Time = normalizeTime(result.Days[i].Services[j].Time)
		}
	}
	return &result, nil
}
