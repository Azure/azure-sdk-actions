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
	CheckSuiteEvent            []byte
	IssueCommentEvent          []byte
	WorkflowRunEvent           []byte
	PullRequestResponse        []byte
	CheckSuiteResponse         []byte
	MultipleCheckSuiteResponse []byte
	StatusResponse             []byte
	NewCommentResponse         []byte
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
	payloads.WorkflowRunEvent, err = ioutil.ReadFile("./testpayloads/workflow_run_event.json")
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
	payloads.MultipleCheckSuiteResponse, err = ioutil.ReadFile("./testpayloads/multiple_check_suite_response.json")
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

func getStatusBody(assert *assert.Assertions, req *http.Request) StatusBody {
	body, err := ioutil.ReadAll(req.Body)
	assert.NoError(err)
	status := StatusBody{}
	assert.NoError(json.Unmarshal(body, &status))
	return status
}

func NewCheckSuiteTestServer(
	assert *assert.Assertions,
	payloads Payloads,
	state1 CommitState,
	state2 CommitState,
	postedState *CommitState,
	postedStatus *bool,
) *httptest.Server {
	checkSuite := NewCheckSuiteWebhook(payloads.CheckSuiteEvent)
	assert.NotEmpty(checkSuite)

	fn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response := []byte{}

		if strings.Contains(checkSuite.GetCheckSuiteUrl(), req.URL.String()) {
			response = payloads.MultipleCheckSuiteResponse
			assert.Contains(req.URL.Path, checkSuite.CheckSuite.HeadSha)
			response = []byte(strings.Replace(string(response),
				`"conclusion": "neutral"`, fmt.Sprintf("\"conclusion\": \"%s\"", state1), 1))
			if state2 != "" {
				response = []byte(strings.Replace(string(response),
					`"conclusion": "neutral"`, fmt.Sprintf("\"conclusion\": \"%s\"", state2), 1))
			}
		} else {
			response = payloads.StatusResponse
			assert.Contains(checkSuite.GetStatusesUrl(), req.URL.String())
			assert.Contains(req.URL.Path, checkSuite.CheckSuite.HeadSha)

			assert.Equal("POST", req.Method)
			status := getStatusBody(assert, req)
			*postedState = status.State
			*postedStatus = true
		}

		w.Write(response)
	})

	return httptest.NewServer(fn)
}

type TestCheckSuiteCase struct {
	Description      string
	AppTargets       []string
	InjectState1     CommitState
	InjectState2     CommitState
	ShouldPostStatus bool
	ExpectedStatus   CommitState
	Event            []byte
}

func TestCheckSuite(t *testing.T) {
	assert := assert.New(t)
	payloads, err := getPayloads()
	assert.NoError(err)
	var zeroCommitState CommitState
	singleAppTarget := []string{"octocoders-linter"}
	multiAppTarget := []string{"Octocat App", "Hexacat App"}

	for _, tc := range []TestCheckSuiteCase{
		{"POST success for single suite", singleAppTarget, CommitStateSuccess, "", true, CommitStateSuccess, payloads.CheckSuiteEvent},
		{"skip for main branch", singleAppTarget, CommitStateSuccess, "", false, zeroCommitState, []byte(strings.ReplaceAll(string(payloads.CheckSuiteEvent), `"head_branch": "changes"`, `"head_branch": "main"`))},
		{"POST pending for multiple suite", multiAppTarget, CommitStateSuccess, CommitStatePending, true, CommitStatePending, payloads.CheckSuiteEvent},
		{"POST pending for multiple suite 2", multiAppTarget, CommitStatePending, CommitStateSuccess, true, CommitStatePending, payloads.CheckSuiteEvent},
		{"POST success for multiple suite", multiAppTarget, CommitStateSuccess, CommitStateSuccess, true, CommitStateSuccess, payloads.CheckSuiteEvent},
	} {
		var postedStatus bool
		var postedState CommitState
		server := NewCheckSuiteTestServer(assert, payloads, tc.InjectState1, tc.InjectState2, &postedState, &postedStatus)
		gh, err := NewGithubClient(server.URL, "", tc.AppTargets...)
		assert.NoError(err, tc.Description)
		defer server.Close()
		err = handleEvent(gh, tc.Event)
		assert.NoError(err, tc.Description)
		assert.Equal(tc.ShouldPostStatus, postedStatus, tc.Description)
		assert.Equal(tc.ExpectedStatus, postedState, tc.Description)
	}
}

type TestCommentCase struct {
	InputComment      string
	InjectConclusion  CheckSuiteConclusion
	ExpectedState     CommitState
	ShouldPostStatus  bool
	ShouldPostComment bool
	AppTargets        []string
	TestDescription   string
}

func TestComments(t *testing.T) {
	payloads, err := getPayloads()
	assert.NoError(t, err)
	issueCommentEvent := NewIssueCommentWebhook(payloads.IssueCommentEvent)
	assert.NotEmpty(t, issueCommentEvent)
	pullRequestResponse := NewPullRequest(payloads.PullRequestResponse)
	assert.NotEmpty(t, pullRequestResponse)

	apps := []string{"Octocat App"}

	cases := []TestCommentCase{
		{"/check-enforcer override", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false, apps, "override+success"},
		{"/check-enforcer override", CheckSuiteConclusionFailure, CommitStateSuccess, true, false, apps, "override+failure"},
		{"   /check-enforcer   override   ", CheckSuiteConclusionFailure, CommitStateSuccess, true, false, apps, "comment spaces"},
		{"/check-enforcer reset", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false, apps, "reset+success"},
		{"/check-enforcer reset", CheckSuiteConclusionFailure, CommitStatePending, true, false, apps, "reset+failure"},
		{"/check-enforcer evaluate", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false, apps, "evaluate+success"},
		{"/check-enforcer evaluate", CheckSuiteConclusionFailure, CommitStatePending, true, false, apps, "evaluate+failure"},
		{"/check-enforcer evaluate", CheckSuiteConclusionTimedOut, CommitStatePending, true, false, apps, "evaluate+timeout"},
		{"/check-enforcer evaluate", CheckSuiteConclusionNeutral, CommitStatePending, true, false, apps, "evaluate+neutral"},
		{"/check-enforcer evaluate", CheckSuiteConclusionStale, CommitStatePending, true, false, apps, "evaluate+stale"},
		{"/check-enforcer evaluate", CheckSuiteConclusionSuccess, CommitStatePending, true, true, []string{"NoMatchApp"}, "evaluate+nopipelinematches"},
		{"/check-enforcer reset", CheckSuiteConclusionSuccess, CommitStatePending, true, true, []string{"NoMatchApp"}, "reset+nopipelinematches"},
		{"/check-enforcer help", "", "", false, true, apps, "help"},
		{"/check-enforcerevaluate", "", "", false, true, apps, "missing space"},
		{"/check-enforcer foobar", "", "", false, true, apps, "invalid command"},
		{"/check-enforcer foobar bar bar", "", "", false, true, apps, "invalid command+args"},
		{"/azp run", "", "", false, false, apps, "different command"},
		{";;;;;;;;;;;;;;;;;;;;;;;;;;;", "", "", false, false, apps, "semicolons"},
	}
	for _, tc := range cases {
		testCommentCase(t, tc, payloads, issueCommentEvent, pullRequestResponse)
	}
}

func testCommentCase(t *testing.T, tc TestCommentCase, payloads Payloads, issueCommentEvent *IssueCommentWebhook, pullRequestResponse *PullRequest) {
	assert := assert.New(t)
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
			assert.Equal("POST", req.Method, "%s: Post new status", tc.TestDescription)
			status := getStatusBody(assert, req)
			assert.Equal(tc.ExpectedState, status.State, tc.TestDescription)
			postedStatus = true
		} else if strings.Contains(issueCommentEvent.GetCommentsUrl(), req.URL.String()) {
			response = payloads.NewCommentResponse
			assert.Equal("POST", req.Method, "%s: POST new comment for command '%s'", tc.TestDescription, tc.InputComment)
			body, err := ioutil.ReadAll(req.Body)
			assert.NoError(err, tc.TestDescription)
			if tc.InputComment == "/check-enforcer evaluate" || tc.InputComment == "/check-enforcer reset" {
				noPipelineText, err := ioutil.ReadFile("./comments/no_pipelines.txt")
				assert.NoError(err, tc.TestDescription)
				expectedBody, err := NewIssueCommentBody(string(noPipelineText))
				assert.NoError(err, tc.TestDescription)
				assert.Equal(string(expectedBody), string(body), "%s: Comment body for command '%s'", tc.TestDescription, tc.InputComment)
			} else {
				helpText, err := ioutil.ReadFile("./comments/help.txt")
				assert.NoError(err, tc.TestDescription)
				expectedBody, err := NewIssueCommentBody(string(helpText))
				assert.NoError(err, tc.TestDescription)
				assert.Equal(string(expectedBody), string(body), "%s: Comment body for command '%s'", tc.TestDescription, tc.InputComment)
			}
			postedComment = true
		} else {
			assert.Fail("Unexpected request to "+req.URL.String(), tc.TestDescription)
		}
		w.Write(response)
	})
	server := httptest.NewServer(fn)
	defer server.Close()

	gh, err := NewGithubClient(server.URL, "", tc.AppTargets...)
	assert.NoError(err, tc.TestDescription)

	replaced := strings.ReplaceAll(string(payloads.IssueCommentEvent), "You are totally right! I'll get this fixed right away.", tc.InputComment)

	err = handleEvent(gh, []byte(replaced))
	assert.NoError(err)
	assert.Equal(tc.ShouldPostStatus, postedStatus, "%s: Should POST status for command '%s'", tc.TestDescription, tc.InputComment)
	assert.Equal(tc.ShouldPostComment, postedComment, "%s: Should POST comment for command '%s'", tc.TestDescription, tc.InputComment)
}

func TestWorkflowRun(t *testing.T) {
	// TODO: Add tests that check the multiple app case, with calling out to check-suites instead of just the
	// webhook event data. Same thing for workflow run
	assert := assert.New(t)
	payloads, err := getPayloads()
	assert.NoError(err)
	workflowRun := NewWorkflowRunWebhook(payloads.WorkflowRunEvent)
	assert.NotEmpty(workflowRun)

	var postedStatus CommitState
	var csResponse string

	fn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response := []byte{}

		if strings.Contains(workflowRun.GetCheckSuiteUrl(), req.URL.String()) {
			response = []byte(csResponse)
		} else {
			assert.Contains(workflowRun.GetStatusesUrl(), req.URL.String())
			assert.Contains(req.URL.Path, workflowRun.HeadSha)

			assert.Equal("POST", req.Method)
			status := getStatusBody(assert, req)
			postedStatus = status.State
			response = payloads.StatusResponse
		}

		w.Write(response)
	})
	server := httptest.NewServer(fn)
	defer server.Close()

	gh, err := NewGithubClient(server.URL, "", "Octocat App")
	assert.NoError(err)

	csResponse = strings.ReplaceAll(
		string(payloads.CheckSuiteResponse), `"conclusion": "neutral"`, fmt.Sprintf("\"conclusion\": \"%s\"", CommitStateSuccess))
	err = handleEvent(gh, payloads.WorkflowRunEvent)
	assert.NoError(err)
	assert.Equal(CommitStateSuccess, postedStatus, "Should POST success status")

	// Test skip check suite events for runs not from pull requests
	replaced := strings.ReplaceAll(string(payloads.WorkflowRunEvent), `"event": "pull_request"`, `"event": "push"`)
	postedStatus = ""
	err = handleEvent(gh, []byte(replaced))
	assert.NoError(err)
	assert.Equal(CommitStatePending, postedStatus, "Should POST pending status")
}
