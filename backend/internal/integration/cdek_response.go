package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type cdekIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type cdekOrderResponse struct {
	Entity struct {
		UUID       string `json:"uuid"`
		CDEKNumber string `json:"cdek_number"`
		Statuses   []struct {
			Code     string `json:"code"`
			Name     string `json:"name"`
			DateTime string `json:"date_time"`
		} `json:"statuses"`
	} `json:"entity"`
	Errors   []cdekIssue `json:"errors"`
	Warnings []cdekIssue `json:"warnings"`
	Requests []struct {
		Type     string      `json:"type"`
		State    string      `json:"state"`
		Errors   []cdekIssue `json:"errors"`
		Warnings []cdekIssue `json:"warnings"`
	} `json:"requests"`
}

func (response cdekOrderResponse) issues() []cdekIssue {
	issues := append(append([]cdekIssue{}, response.Errors...), response.Warnings...)
	for _, request := range response.Requests {
		issues = append(issues, request.Errors...)
		issues = append(issues, request.Warnings...)
	}
	return issues
}

func onlyCDEKNotFound(issues []cdekIssue) bool {
	if len(issues) == 0 {
		return false
	}
	for _, issue := range issues {
		if issue.Code != "v2_entity_not_found" && issue.Code != "v2_order_not_found" {
			return false
		}
	}
	return true
}

func (response cdekOrderResponse) failure() error {
	if onlyCDEKNotFound(response.issues()) {
		return ErrCDEKOrderNotFound
	}
	issues := append([]cdekIssue{}, response.Errors...)
	for _, request := range response.Requests {
		if request.State == "INVALID" && len(request.Errors) == 0 {
			return fmt.Errorf("СДЭК отклонил запрос %s без пояснения", request.Type)
		}
		issues = append(issues, request.Errors...)
	}
	if len(issues) > 0 {
		return fmt.Errorf("ошибка СДЭК: %s", cdekIssueText(issues))
	}
	return nil
}

func cdekIssueText(issues []cdekIssue) string {
	parts := []string{}
	for _, issue := range issues {
		if len(parts) == 4 {
			break
		}
		parts = append(parts, truncateRunes(strings.TrimSpace(issue.Code+": "+issue.Message), 200))
	}
	return strings.Join(parts, "; ")
}

func cdekResponseError(response *http.Response, request *http.Request) error {
	var result cdekOrderResponse
	_ = json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result)
	issues := result.issues()
	// Forbidden, server errors and arbitrary 400 responses are not proof of
	// deletion. The documented not-found codes are accepted only on lookup.
	lookup := request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v2/orders")
	if lookup && (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusBadRequest && onlyCDEKNotFound(issues)) {
		if len(issues) == 0 || onlyCDEKNotFound(issues) {
			return ErrCDEKOrderNotFound
		}
	}
	message := fmt.Sprintf("СДЭК вернул ошибку (%d)", response.StatusCode)
	if len(issues) > 0 {
		message += ": " + cdekIssueText(issues)
	}
	if response.StatusCode >= 500 {
		return fmt.Errorf("%w: %s", ErrCDEKOutcomeUnknown, message)
	}
	return fmt.Errorf("%s", message)
}

// Failed historical DELETE/UPDATE requests do not invalidate a current GET.
func (response cdekOrderResponse) lookupFailure() error {
	lookup := cdekOrderResponse{Errors: response.Errors, Warnings: response.Warnings}
	for _, request := range response.Requests {
		if request.Type == "GET" || request.Type == "" {
			lookup.Requests = append(lookup.Requests, request)
		}
	}
	return lookup.failure()
}
