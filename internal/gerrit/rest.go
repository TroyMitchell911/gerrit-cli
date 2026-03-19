package gerrit

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/drakeaharper/gerrit-cli/internal/config"
)

type RESTClient struct {
	config     *config.Config
	httpClient *http.Client
}

func NewRESTClient(cfg *config.Config) *RESTClient {
	return NewRESTClientWithTimeout(cfg, 30*time.Second)
}

func NewRESTClientWithTimeout(cfg *config.Config, timeout time.Duration) *RESTClient {
	return &RESTClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *RESTClient) getBaseURL() string {
	// Use the config's method to build the URL properly
	// Remove the /a/ part as we'll add it in doRequest
	url := c.config.GetRESTURL("")
	return strings.TrimSuffix(url, "/a/")
}

func (c *RESTClient) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	url := fmt.Sprintf("%s/a/%s", c.getBaseURL(), strings.TrimPrefix(path, "/"))

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// Add basic auth
	auth := base64.StdEncoding.EncodeToString([]byte(c.config.User + ":" + c.config.HTTPPassword))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch resp.StatusCode {
		case 401:
			return nil, fmt.Errorf("authentication failed (401) - check your HTTP password")
		case 403:
			return nil, fmt.Errorf("access forbidden (403) - check your permissions")
		case 404:
			return nil, fmt.Errorf("endpoint not found (404) - check server URL and port")
		default:
			return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		}
	}

	return resp, nil
}

func (c *RESTClient) Get(path string) ([]byte, error) {
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Gerrit REST API returns ")]}'" as XSS protection prefix
	if bytes.HasPrefix(body, []byte(")]}'")) {
		body = body[4:]
	}

	return body, nil
}

func (c *RESTClient) Post(path string, data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	resp, err := c.doRequest("POST", path, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Remove XSS protection prefix
	if bytes.HasPrefix(body, []byte(")]}'")) {
		body = body[4:]
	}

	return body, nil
}

func (c *RESTClient) Put(path string, data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	resp, err := c.doRequest("PUT", path, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Remove XSS protection prefix
	if bytes.HasPrefix(body, []byte(")]}'")) {
		body = body[4:]
	}

	return body, nil
}

func (c *RESTClient) Delete(path string) error {
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// TestConnection tests the REST API connection
func (c *RESTClient) TestConnection() error {
	// Try to get server version
	resp, err := c.Get("config/server/version")
	if err != nil {
		return fmt.Errorf("failed to connect to Gerrit REST API: %w", err)
	}

	// Just check if we got a response
	if len(resp) == 0 {
		return fmt.Errorf("empty response from Gerrit server")
	}

	return nil
}

// GetChange retrieves a change by ID
func (c *RESTClient) GetChange(changeID string) (map[string]interface{}, error) {
	resp, err := c.Get(fmt.Sprintf("changes/%s?o=DETAILED_LABELS&o=CURRENT_REVISION&o=CURRENT_COMMIT&o=DETAILED_ACCOUNTS", changeID))
	if err != nil {
		return nil, err
	}

	var change map[string]interface{}
	if err := json.Unmarshal(resp, &change); err != nil {
		return nil, fmt.Errorf("failed to parse change: %w", err)
	}

	return change, nil
}

// GetChangeComments retrieves comments for a change
func (c *RESTClient) GetChangeComments(changeID string) (map[string]interface{}, error) {
	resp, err := c.Get(fmt.Sprintf("changes/%s/comments", changeID))
	if err != nil {
		return nil, err
	}

	var comments map[string]interface{}
	if err := json.Unmarshal(resp, &comments); err != nil {
		return nil, fmt.Errorf("failed to parse comments: %w", err)
	}

	return comments, nil
}

// ListChanges lists changes based on query
func (c *RESTClient) ListChanges(query string, limit int) ([]map[string]interface{}, error) {
	path := fmt.Sprintf("changes/?q=%s&n=%d&o=LABELS&o=CURRENT_REVISION&o=DETAILED_ACCOUNTS", query, limit)
	resp, err := c.Get(path)
	if err != nil {
		return nil, err
	}

	var changes []map[string]interface{}
	if err := json.Unmarshal(resp, &changes); err != nil {
		return nil, fmt.Errorf("failed to parse changes: %w", err)
	}

	return changes, nil
}

// GetChangeFiles retrieves the list of files in a change
func (c *RESTClient) GetChangeFiles(changeID string, revision string) (map[string]interface{}, error) {
	path := fmt.Sprintf("changes/%s/revisions/%s/files", changeID, revision)
	resp, err := c.Get(path)
	if err != nil {
		return nil, err
	}

	var files map[string]interface{}
	if err := json.Unmarshal(resp, &files); err != nil {
		return nil, fmt.Errorf("failed to parse files: %w", err)
	}

	return files, nil
}

// GetChangeMessages retrieves all messages for a change
func (c *RESTClient) GetChangeMessages(changeID string) ([]map[string]interface{}, error) {
	path := fmt.Sprintf("changes/%s/messages", changeID)
	resp, err := c.Get(path)
	if err != nil {
		return nil, err
	}

	var messages []map[string]interface{}
	if err := json.Unmarshal(resp, &messages); err != nil {
		return nil, fmt.Errorf("failed to parse messages: %w", err)
	}

	return messages, nil
}

// PostReview posts a review comment on a change
func (c *RESTClient) PostReview(changeID string, revision string, message string) error {
	path := fmt.Sprintf("changes/%s/revisions/%s/review", changeID, revision)
	data := map[string]interface{}{
		"message": message,
	}

	_, err := c.Post(path, data)
	return err
}

// AddReviewer adds a reviewer or CC to a change
// state should be "REVIEWER" or "CC"
func (c *RESTClient) AddReviewer(changeID string, reviewer string, state string) error {
	path := fmt.Sprintf("changes/%s/reviewers", changeID)
	data := map[string]interface{}{
		"reviewer": reviewer,
		"state":    state,
	}

	_, err := c.Post(path, data)
	return err
}

// RebaseChange rebases a change onto a new base
// base can be empty (rebase onto target branch HEAD), a commit SHA-1, or "change~patchset"
// allowConflicts allows rebasing even with conflicts (creates conflict markers)
func (c *RESTClient) RebaseChange(changeID string, base string, allowConflicts bool) (map[string]interface{}, error) {
	path := fmt.Sprintf("changes/%s/rebase", changeID)
	data := map[string]interface{}{}

	if base != "" {
		data["base"] = base
	}
	if allowConflicts {
		data["allow_conflicts"] = true
	}

	resp, err := c.Post(path, data)
	if err != nil {
		return nil, err
	}

	var change map[string]interface{}
	if err := json.Unmarshal(resp, &change); err != nil {
		return nil, fmt.Errorf("failed to parse rebase response: %w", err)
	}

	return change, nil
}

// ListProjects retrieves all projects
func (c *RESTClient) ListProjects() (map[string]interface{}, error) {
	resp, err := c.Get("projects/")
	if err != nil {
		return nil, err
	}

	var projects map[string]interface{}
	if err := json.Unmarshal(resp, &projects); err != nil {
		return nil, fmt.Errorf("failed to parse projects: %w", err)
	}

	return projects, nil
}

// ReplyComment posts a reply to an existing inline comment (creates draft then publishes)
func (c *RESTClient) ReplyComment(changeID, revision, path, inReplyTo, message string) error {
	apiPath := fmt.Sprintf("changes/%s/revisions/%s/drafts", changeID, revision)
	body := map[string]interface{}{
		"path":        path,
		"in_reply_to": inReplyTo,
		"message":     message,
		"unresolved":  false,
	}
	if _, err := c.Put(apiPath, body); err != nil {
		return err
	}
	return c.PublishDraftComments(changeID, revision)
}

func (c *RESTClient) CreateDraftComment(changeID, revision string, comment map[string]interface{}) error {
	path := fmt.Sprintf("changes/%s/revisions/%s/drafts", changeID, revision)
	_, err := c.Put(path, comment)
	return err
}

// PublishDraftComments publishes all draft comments by posting an empty reply
func (c *RESTClient) PublishDraftComments(changeID, revision string) error {
	path := fmt.Sprintf("changes/%s/revisions/%s/review", changeID, revision)
	body := map[string]interface{}{
		"message": "",
		"drafts":  "PUBLISH",
	}
	_, err := c.Post(path, body)
	return err
}

// SubmitReview submits a label vote on a change
func (c *RESTClient) SubmitReview(changeID, revision, label string, value int) error {
	path := fmt.Sprintf("changes/%s/revisions/%s/review", changeID, revision)
	body := map[string]interface{}{
		"labels": map[string]int{
			label: value,
		},
	}
	_, err := c.Post(path, body)
	return err
}

func (c *RESTClient) GetFileDiff(changeID, revision, filename string) (string, error) {
	// URL-encode the filename
	encodedFile := strings.ReplaceAll(filename, "/", "%2F")
	path := fmt.Sprintf("changes/%s/revisions/%s/files/%s/diff", changeID, revision, encodedFile)

	resp, err := c.Get(path)
	if err != nil {
		return "", err
	}

	var diff map[string]interface{}
	if err := json.Unmarshal(resp, &diff); err != nil {
		return "", fmt.Errorf("failed to parse diff: %w", err)
	}

	// Format as unified diff
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n", filename))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", filename))

	if content, ok := diff["content"].([]interface{}); ok {
		lineA, lineB := 1, 1
		for _, section := range content {
			if s, ok := section.(map[string]interface{}); ok {
				if ab, ok := s["ab"].([]interface{}); ok {
					// Context lines
					sb.WriteString(fmt.Sprintf("@@ -%d +%d @@\n", lineA, lineB))
					for _, l := range ab {
						sb.WriteString(fmt.Sprintf(" %v\n", l))
						lineA++
						lineB++
					}
				}
				if a, ok := s["a"].([]interface{}); ok {
					sb.WriteString(fmt.Sprintf("@@ -%d +%d @@\n", lineA, lineB))
					for _, l := range a {
						sb.WriteString(fmt.Sprintf("-%v\n", l))
						lineA++
					}
				}
				if b, ok := s["b"].([]interface{}); ok {
					for _, l := range b {
						sb.WriteString(fmt.Sprintf("+%v\n", l))
						lineB++
					}
				}
			}
		}
	}

	return sb.String(), nil
}

// SearchAccounts searches for Gerrit accounts by query string
func (c *RESTClient) SearchAccounts(query string) ([]map[string]interface{}, error) {
	path := fmt.Sprintf("accounts/?suggest&q=%s&n=10", url.QueryEscape(query))
	resp, err := c.Get(path)
	if err != nil {
		return nil, err
	}
	var accounts []map[string]interface{}
	if err := json.Unmarshal(resp, &accounts); err != nil {
		return nil, fmt.Errorf("failed to parse accounts: %w", err)
	}
	return accounts, nil
}

// AbandonChange abandons a change
func (c *RESTClient) AbandonChange(changeID string, message string) error {
	path := fmt.Sprintf("changes/%s/abandon", changeID)
	data := map[string]interface{}{}
	if message != "" {
		data["message"] = message
	}
	_, err := c.Post(path, data)
	return err
}
