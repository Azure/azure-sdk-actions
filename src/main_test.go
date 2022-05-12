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
	return payloads, nil
}

func TestCheckSuite(t *testing.T) {
	payloads, err := getPayloads()
	assert.NoError(t, err)
	cs := NewCheckSuiteWebhook(payloads.CheckSuiteEvent)
	assert.NotEmpty(t, cs)

	postedStatus := false

	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, cs.GetStatusesUrl(), r.URL.String())
		assert.Contains(t, r.URL.Path, cs.CheckSuite.HeadSha)

		assert.Equal(t, "POST", r.Method)
		status := getBody(t, r)
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
	Comment       string
	Conclusion    CheckSuiteConclusion
	ExpectedState CommitState
	PostStatus    bool
}

func TestComments(t *testing.T) {
	payloads, err := getPayloads()
	assert.NoError(t, err)
	ic := NewIssueCommentWebhook(payloads.IssueCommentEvent)
	assert.NotEmpty(t, ic)
	pr := NewPullRequest(payloads.PullRequestResponse)
	assert.NotEmpty(t, pr)

	cases := []testCommentCaseConfig{
		{"/check-enforcer override", CheckSuiteConclusionSuccess, CommitStateSuccess, true},
		{"/check-enforcer override", CheckSuiteConclusionFailure, CommitStateSuccess, true},
		{"   /check-enforcer   override   ", CheckSuiteConclusionFailure, CommitStateSuccess, true},
		{"/check-enforcer reset", CheckSuiteConclusionSuccess, CommitStatePending, true},
		{"/check-enforcer reset", CheckSuiteConclusionFailure, CommitStatePending, true},
		{"/check-enforcer evaluate", CheckSuiteConclusionSuccess, CommitStateSuccess, true},
		{"/check-enforcer evaluate", CheckSuiteConclusionFailure, CommitStateFailure, true},
		{"/check-enforcer evaluate", CheckSuiteConclusionTimedOut, CommitStateFailure, true},
		{"/check-enforcer evaluate", CheckSuiteConclusionNeutral, CommitStatePending, true},
		{"/check-enforcer evaluate", CheckSuiteConclusionStale, CommitStatePending, true},
		{"/check-enforcer evaluate", "", CommitStatePending, true},
		{"/check-enforcer foobar", "", "", false},
		{"/azp run", "", "", false},
		{";;;;;;;;;;;;;;;;;;;;;;;;;;;", "", "", false},
	}
	for _, tc := range cases {
		testCommentCase(t, tc, payloads, ic, pr)
	}
}

func testCommentCase(t *testing.T, tc testCommentCaseConfig, payloads Payloads, ic *IssueCommentWebhook, pr *PullRequest) {
	postedStatus := false

	csResponse := strings.ReplaceAll(
		string(payloads.CheckSuiteResponse),
		`"conclusion": "neutral"`,
		fmt.Sprintf("\"conclusion\": \"%s\"", tc.Conclusion))

	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []byte{}
		if strings.Contains(ic.GetPullsUrl(), r.URL.String()) {
			response = payloads.PullRequestResponse
		} else if strings.Contains(pr.GetCheckSuiteUrl(), r.URL.String()) {
			response = []byte(csResponse)
		} else if strings.Contains(pr.GetStatusesUrl(), r.URL.String()) {
			response = payloads.StatusResponse
			assert.Equal(t, "POST", r.Method)
			status := getBody(t, r)
			assert.Equal(t, tc.ExpectedState, status.State, "TestCase %s", tc.Comment)
			postedStatus = true
		} else {
			assert.Fail(t, "Unexpected request to "+r.URL.String())
		}
		w.Write(response)
	})
	server := httptest.NewServer(fn)
	defer server.Close()

	gh, err := NewGithubClient(server.URL, "", "Octocat App")
	assert.NoError(t, err)

	replaced := strings.ReplaceAll(string(payloads.IssueCommentEvent), "You are totally right! I'll get this fixed right away.", tc.Comment)

	err = handleEvent(gh, []byte(replaced))
	assert.NoError(t, err)
	assert.Equal(t, tc.PostStatus, postedStatus, "Should POST status")
}

func getBody(t *testing.T, r *http.Request) StatusBody {
	body, err := ioutil.ReadAll(r.Body)
	assert.NoError(t, err)
	status := StatusBody{}
	assert.NoError(t, json.Unmarshal(body, &status))
	return status
}
