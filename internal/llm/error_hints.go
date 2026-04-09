package llm

import (
	"errors"
	"fmt"
	"strings"
)

type apiStatusError struct {
	StatusCode int
	Body       string
}

func (e *apiStatusError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("API %d", e.StatusCode)
	}
	return fmt.Sprintf("API %d: %s", e.StatusCode, body)
}

func explainRequestFailure(provider, baseURL string, attempts int, err error) error {
	if err == nil {
		return nil
	}

	message := err.Error()
	if attempts > 0 {
		message = fmt.Sprintf("after %d attempts: %v", attempts, err)
	}

	if hint := transportErrorHint(provider, baseURL, err); hint != "" {
		message += "\nHint: " + hint
	}

	return errors.New(message)
}

func transportErrorHint(provider, baseURL string, err error) string {
	if err == nil {
		return ""
	}

	if apiErr, ok := err.(*apiStatusError); ok {
		switch apiErr.StatusCode {
		case 404:
			switch strings.TrimSpace(strings.ToLower(provider)) {
			case "anthropic":
				return "This transport posts to " + transportEndpointPreview(provider, baseURL) + ". A 404 usually means the server is OpenAI-compatible instead. Try /provider openai or point /baseurl at an Anthropic-compatible root."
			default:
				return "This transport posts to " + transportEndpointPreview(provider, baseURL) + ". A 404 usually means the server is Anthropic-compatible instead. Try /provider anthropic or point /baseurl at an OpenAI-compatible root."
			}
		case 429:
			return "The upstream API rate limited this request. Check the active API key/profile, quota, or billing status and try again after a short wait."
		case 401, 403:
			return "The upstream API rejected credentials or access. Recheck /apikey, the active profile, and whether the selected provider matches the endpoint."
		}
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "connection refused"):
		return "Could not reach " + transportEndpointPreview(provider, baseURL) + ". Check /baseurl, make sure the server is running, and confirm /provider matches the server type."
	case strings.Contains(lower, "no such host"):
		return "The configured host could not be resolved. Recheck /baseurl for typos."
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "The request timed out before the server completed the response. Recheck /baseurl, network reachability, and upstream load."
	}

	return ""
}

func transportEndpointPreview(provider, baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "anthropic":
		return baseURL + "/messages"
	default:
		return baseURL + "/chat/completions"
	}
}
