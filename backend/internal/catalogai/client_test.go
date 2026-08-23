package catalogai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestGenerateUsesFastStructuredRequest(t *testing.T) {
	client := New("secret", "gpt-5-mini")
	client.http = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, exists := body["tools"]; exists {
			t.Fatal("fast card generation must not run web search")
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		if reasoning["effort"] != "low" {
			t.Fatalf("unexpected reasoning configuration: %#v", reasoning)
		}
		payload := `{"name":"Аглаонема Мария","latinName":"Aglaonema commutatum 'Maria'","shortDescription":"Коротко","description":"Описание","careInstructions":"Уход","attributes":{"light":"diffused"},"passport":{"origin":"Азия","lighting":"Рассеянный свет","watering":"Умеренный","humidity":"Средняя","temperature":"18–26 °C","soil":"Рыхлый","fertilizer":"Весной и летом","repotting":"Раз в 2 года","careDifficulty":"Лёгкий","growthRate":"Средний","matureSize":"До 80 см","toxicity":"Токсично","problems":"Перелив","pests":"Клещ","faq":[{"question":"Куда поставить?","answer":"В рассеянный свет."}]},"warnings":["Беречь от животных"],"coverPrompt":"studio plant"}`
		envelope, _ := json.Marshal(map[string]any{"output": []any{map[string]any{"content": []any{map[string]any{"type": "output_text", "text": payload}}}}})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(envelope))), Header: make(http.Header)}, nil
	})}

	proposal, err := client.Generate(context.Background(), Input{Name: "Аглаонема Мария D11"}, "care")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Passport.FAQ[0].Question == "" {
		t.Fatalf("proposal was not decoded: %#v", proposal)
	}
}
