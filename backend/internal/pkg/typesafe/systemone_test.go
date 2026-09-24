package typesafe

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSystemOneRequestValidQuestionTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"noul string state", `{"model":"jev-latest","state":"sample","questions":{"safety":{"type":"noul","instructions":"Evaluate safety","criteria":{"safe":"No harm"}}}}`},
		{"choice object state", `{"model":"jev-latest","state":{"text":"sample"},"questions":{"label":{"type":"choice","instructions":{"task":"Classify"},"criteria":{"safe":"Allowed","unsafe":null}}},"stream":false}`},
		{"score array state", `{"model":"jev-latest","state":["sample"],"questions":{"quality":{"type":"score","instructions":["Rate quality"],"criteria":["poor","good"]}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model, err := ValidateSystemOneRequest([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, JevLatestModel, model)
		})
	}
}

func TestValidateSystemOneRequestRejectsOversizedQuestionSet(t *testing.T) {
	questions := make(map[string]Question, maxSystemOneQuestions+1)
	for i := 0; i < maxSystemOneQuestions+1; i++ {
		questions[string(rune('a'+i%26))+string(rune('0'+i/26))] = Question{Type: "noul", Instructions: "x"}
	}
	body, err := json.Marshal(map[string]any{
		"model": "jev-latest", "state": "x", "questions": questions,
	})
	require.NoError(t, err)
	_, err = ValidateSystemOneRequest(body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most")
}

func TestValidateSystemOneRequestRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"invalid json", `{`, "invalid JSON"},
		{"missing model", `{"state":"x","questions":{"q":{"type":"noul","instructions":"x"}}}`, "model"},
		{"illegal model", `{"model":"jev-old","state":"x","questions":{"q":{"type":"noul","instructions":"x"}}}`, "jev-latest"},
		{"model with whitespace", `{"model":" jev-latest ","state":"x","questions":{"q":{"type":"noul","instructions":"x"}}}`, "jev-latest"},
		{"missing state", `{"model":"jev-latest","questions":{"q":{"type":"noul","instructions":"x"}}}`, "state"},
		{"scalar state", `{"model":"jev-latest","state":42,"questions":{"q":{"type":"noul","instructions":"x"}}}`, "state"},
		{"empty questions", `{"model":"jev-latest","state":"x","questions":{}}`, "questions"},
		{"unknown question type", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"boolean","instructions":"x"}}}`, "unsupported type"},
		{"question type with whitespace", `{"model":"jev-latest","state":"x","questions":{"q":{"type":" noul ","instructions":"x"}}}`, "unsupported type"},
		{"noul criteria array", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul","instructions":"x","criteria":[]}}}`, "noul criteria"},
		{"noul criteria null", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul","instructions":"x","criteria":null}}}`, "noul criteria"},
		{"empty choice criteria", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"choice","instructions":"x","criteria":{}}}}`, "choice criteria"},
		{"choice numeric value", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"choice","instructions":"x","criteria":{"one":1}}}}`, "choice criteria"},
		{"short score criteria", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"score","instructions":"x","criteria":["only"]}}}`, "score criteria"},
		{"non-string score criteria", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"score","instructions":"x","criteria":["low",2]}}}`, "score criteria"},
		{"stream true", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul","instructions":"x"}},"stream":true}`, "streaming"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateSystemOneRequest([]byte(tc.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			if tc.name == "stream true" {
				require.True(t, errors.Is(err, ErrStreamingUnsupported))
			}
		})
	}
}
