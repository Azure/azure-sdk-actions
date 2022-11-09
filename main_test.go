package main

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type Payloads struct {
	CheckSuiteEvent     []byte
	IssueCommentEvent   []byte
	PullRequestResponse []byte
	CheckSuiteResponse  []byte
	StatusResponse      []byte
	NewCommentResponse  []byte
}

func getPayloads() (Payloads, error) {
	payloads := Payloads{}
	var err error
	payloads.CheckSuiteEvent, err = ioutil.ReadFile("./testpayloads/check_suite_event.json")
	if err != nil {
		return Payloads{}, err
	}
	payloads.IssueCommentEvent, err = ioutil.ReadFile("./testpayloads/issue_comment_event.json")
	if err != nil {
		return Payloads{}, err
	}
	payloads.PullRequestResponse, err = ioutil.ReadFile("./testpayloads/pull_request_response.json")
	if err != nil {
		return Payloads{}, err
	}
	payloads.CheckSuiteResponse, err = ioutil.ReadFile("./testpayloads/check_suite_response.json")
	if err != nil {
		return Payloads{}, err
	}
	payloads.StatusResponse, err = ioutil.ReadFile("./testpayloads/status_response.json")
	if err != nil {
		return Payloads{}, err
	}
	payloads.StatusResponse, err = ioutil.ReadFile("./testpayloads/new_comment_response.json")
	if err != nil {
		return Payloads{}, err
	}
	return payloads, nil
}

func TestCheckSuite(t *testing.T) {
	payloads, err := getPayloads()
	assert.NoError(t, err)
	cs := NewCheckSuiteWebhook(payloads.CheckSuiteEvent)
	assert.NotEmpty(t, cs)

	postedStatus := false

	fn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Contains(t, cs.GetStatusesUrl(), req.URL.String())
		assert.Contains(t, req.URL.Path, cs.CheckSuite.HeadSha)

		assert.Equal(t, "POST", req.Method)
		status := getStatusBody(t, req)
		assert.Equal(t, status.State, CommitStateSuccess)
		postedStatus = true

		w.Write(payloads.StatusResponse)
	})
	server := httptest.NewServer(fn)
	defer server.Close()

	gh, err := NewGithubClient(server.URL, "", "octocoders-linter")
	assert.NoError(t, err)

	err = handleEvent(gh, payloads.CheckSuiteEvent)
	assert.NoError(t, err)
	assert.True(t, postedStatus, "Should POST status")

	// Test skip check suite events for main branch
	replaced := strings.ReplaceAll(string(payloads.CheckSuiteEvent), `"head_branch": "changes"`, `"head_branch": "main"`)
	postedStatus = false
	err = handleEvent(gh, []byte(replaced))
	assert.NoError(t, err)
	assert.False(t, postedStatus, "Should POST status")
}

type testCommentCaseConfig struct {
	InputComment      string
	InjectConclusion  CheckSuiteConclusion
	ExpectedState     CommitState
	ShouldPostStatus  bool
	ShouldPostComment bool
}

func TestComments(t *testing.T) {
	payloads, err := getPayloads()
	assert.NoError(t, err)
	issueCommentEvent := NewIssueCommentWebhook(payloads.IssueCommentEvent)
	assert.NotEmpty(t, issueCommentEvent)
	pullRequestResponse := NewPullRequest(payloads.PullRequestResponse)
	assert.NotEmpty(t, pullRequestResponse)

	cases := []testCommentCaseConfig{
		{"/check-enforcer override", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false},
		{"/check-enforcer override", CheckSuiteConclusionFailure, CommitStateSuccess, true, false},
		{"   /check-enforcer   override   ", CheckSuiteConclusionFailure, CommitStateSuccess, true, false},
		{"/check-enforcer reset", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false},
		{"/check-enforcer reset", CheckSuiteConclusionFailure, CommitStatePending, true, false},
		{"/check-enforcer evaluate", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false},
		{"/check-enforcer evaluate", CheckSuiteConclusionFailure, CommitStatePending, true, false},
		{"/check-enforcer evaluate", CheckSuiteConclusionTimedOut, CommitStatePending, true, false},
		{"/check-enforcer evaluate", CheckSuiteConclusionNeutral, CommitStatePending, true, false},
		{"/check-enforcer evaluate", CheckSuiteConclusionStale, CommitStatePending, true, false},
		{"/check-enforcer evaluate", "", CommitStatePending, true, true},
		{"/check-enforcer reset", "", CommitStatePending, true, true},
		{"/check-enforcer help", "", "", false, true},
		{"/check-enforcerevaluate", "", "", false, true},
		{"/check-enforcer foobar", "", "", false, true},
		{"/check-enforcer foobar bar bar", "", "", false, true},
		{"/azp run", "", "", false, false},
		{";;;;;;;;;;;;;;;;;;;;;;;;;;;", "", "", false, false},
	}
	for _, tc := range cases {
		testCommentCase(t, tc, payloads, issueCommentEvent, pullRequestResponse)
	}
}

func testCommentCase(t *testing.T, tc testCommentCaseConfig, payloads Payloads, issueCommentEvent *IssueCommentWebhook, pullRequestResponse *PullRequest) {
	postedStatus := false
	postedComment := false

	csResponse := strings.ReplaceAll(
		string(payloads.CheckSuiteResponse),
		`"conclusion": "neutral"`,
		fmt.Sprintf("\"conclusion\": \"%s\"", tc.InjectConclusion))

	fn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response := []byte{}
		if strings.Contains(issueCommentEvent.GetPullsUrl(), req.URL.String()) {
			response = payloads.PullRequestResponse
		} else if strings.Contains(pullRequestResponse.GetCheckSuiteUrl(), req.URL.String()) {
			response = []byte(csResponse)
		} else if strings.Contains(pullRequestResponse.StatusesUrl, req.URL.String()) {
			response = payloads.StatusResponse
			assert.Equal(t, "POST", req.Method, "Post new status")
			status := getStatusBody(t, req)
			assert.Equal(t, tc.ExpectedState, status.State, "TestCase %s", tc.InputComment)
			postedStatus = true
		} else if strings.Contains(issueCommentEvent.GetCommentsUrl(), req.URL.String()) {
			response = payloads.NewCommentResponse
			assert.Equal(t, "POST", req.Method, fmt.Sprintf("POST new comment for command '%s'", tc.InputComment))
			body, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)
			if tc.InputComment == "/check-enforcer evaluate" || tc.InputComment == "/check-enforcer reset" {
				noPipelineText, err := ioutil.ReadFile("./comments/no_pipelines.txt")
				assert.NoError(t, err)
				expectedBody, err := NewIssueCommentBody(string(noPipelineText))
				assert.NoError(t, err)
				assert.Equal(t, string(expectedBody), string(body), fmt.Sprintf("Comment body for command '%s'", tc.InputComment))
			} else {
				helpText, err := ioutil.ReadFile("./comments/help.txt")
				assert.NoError(t, err)
				expectedBody, err := NewIssueCommentBody(string(helpText))
				assert.NoError(t, err)
				assert.Equal(t, string(expectedBody), string(body), fmt.Sprintf("Comment body for command '%s'", tc.InputComment))
			}
			postedComment = true
		} else {
			assert.Fail(t, "Unexpected request to "+req.URL.String())
		}
		w.Write(response)
	})
	server := httptest.NewServer(fn)
	defer server.Close()

	gh, err := NewGithubClient(server.URL, "", "Octocat App")
	assert.NoError(t, err)

	replaced := strings.ReplaceAll(string(payloads.IssueCommentEvent), "You are totally right! I'll get this fixed right away.", tc.InputComment)

	err = handleEvent(gh, []byte(replaced))
	assert.NoError(t, err)
	assert.Equal(t, tc.ShouldPostStatus, postedStatus, fmt.Sprintf("Should POST status for command '%s'", tc.InputComment))
	assert.Equal(t, tc.ShouldPostComment, postedComment, fmt.Sprintf("Should POST comment for command '%s'", tc.InputComment))
}

func getStatusBody(t *testing.T, req *http.Request) StatusBody {
	body, err := ioutil.ReadAll(req.Body)
	assert.NoError(t, err)
	status := StatusBody{}
	assert.NoError(t, json.Unmarshal(body, &status))
	return status
}
