package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai"
	SystemOnePath  = "/v1/systemone"

	JevLatestModel  = "jev-latest"
	Jev1130Model    = "jev-1.13.0"
	Jev113Model     = "jev-1.13"
	Jev113FreeModel = "jev-1.13-free"
)

var supportedModels = []string{
	JevLatestModel,
	Jev1130Model,
	Jev113Model,
	Jev113FreeModel,
}

// SupportedModels returns the TypeSafe models accepted by System One.
func SupportedModels() []string {
	return append([]string(nil), supportedModels...)
}

func IsSupportedModel(model string) bool {
	for _, supported := range supportedModels {
		if model == supported {
			return true
		}
	}
	return false
}

type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

type Request struct {
	Model     string              `json:"model"`
	State     string              `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type Result struct {
	Model  string
	Scores map[string]float64
	Usage  Usage
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

const maxUsageTokens = 1 << 30

func validateUsage(usage Usage) error {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 ||
		usage.InputTokens > maxUsageTokens || usage.OutputTokens > maxUsageTokens {
		return errors.New("typesafe invalid usage")
	}
	return nil
}

type SystemOneResponse struct {
	Body  []byte
	Model string
	Usage Usage
}

func NewSystemOneRequest(ctx context.Context, baseURL, key string, body []byte) (*http.Request, error) {
	endpoint, err := url.JoinPath(strings.TrimRight(baseURL, "/"), SystemOnePath)
	if err != nil {
		return nil, errors.New("typesafe invalid endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("typesafe invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func DecodeSystemOneResponse(r io.Reader) (*SystemOneResponse, error) {
	body, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil || !json.Valid(body) {
		return nil, errors.New("typesafe invalid response")
	}
	var envelope struct {
		Model string `json:"model"`
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Usage == nil {
		return nil, errors.New("typesafe invalid response")
	}
	if err := validateUsage(*envelope.Usage); err != nil {
		return nil, err
	}
	return &SystemOneResponse{Body: body, Model: envelope.Model, Usage: *envelope.Usage}, nil
}

// Evaluate performs one attempt. The caller owns timeouts, retries and key rotation.
func Evaluate(ctx context.Context, client *http.Client, baseURL, key string, input Request) (*Result, int, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, 0, errors.New("typesafe invalid request")
	}
	req, err := NewSystemOneRequest(ctx, baseURL, key, body)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, errors.New("typesafe transport unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not log provider error bodies: they may echo user input or credentials.
		return nil, resp.StatusCode, fmt.Errorf("typesafe API status %d", resp.StatusCode)
	}
	var out struct {
		Model   string `json:"model"`
		Usage   Usage  `json:"usage"`
		Answers map[string]struct {
			Type string   `json:"type"`
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || strings.TrimSpace(out.Model) == "" {
		return nil, resp.StatusCode, errors.New("typesafe invalid response")
	}
	if err := validateUsage(out.Usage); err != nil {
		return nil, resp.StatusCode, err
	}
	result := &Result{Model: out.Model, Usage: out.Usage, Scores: make(map[string]float64, len(input.Questions))}
	for id := range input.Questions {
		answer, ok := out.Answers[id]
		if !ok || answer.Type != "noul" || answer.Noul == nil || math.IsNaN(*answer.Noul) || math.IsInf(*answer.Noul, 0) || *answer.Noul < 0 || *answer.Noul > 1 {
			return nil, resp.StatusCode, fmt.Errorf("typesafe invalid answer for %s", id)
		}
		result.Scores[id] = *answer.Noul
	}
	return result, resp.StatusCode, nil
}
