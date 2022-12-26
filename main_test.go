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
	NoPipelinesComment         []byte
	HelpComment                []byte
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
	payloads.NoPipelinesComment, err = ioutil.ReadFile("./comments/no_pipelines.txt")
	if err != nil {
		return Payloads{}, err
	}
	payloads.HelpComment, err = ioutil.ReadFile("./comments/help.txt")
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

type TestCheckSuiteCase struct {
	Description      string
	AppTargets       []string
	InjectState1     CommitState
	InjectState2     CommitState
	ShouldPostStatus bool
	ExpectedStatus   CommitState
	Event            []byte
}

func NewCheckSuiteTestServer(
	assert *assert.Assertions,
	payloads Payloads,
	state1 CommitState,
	state2 CommitState,
	postedState *CommitState,
	postedStatus *bool,
	description string,
) *httptest.Server {
	checkSuite := NewCheckSuiteWebhook(payloads.CheckSuiteEvent)
	assert.NotEmpty(checkSuite)

	fn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response := []byte{}

		if strings.Contains(checkSuite.GetCheckSuiteUrl(), req.URL.String()) && req.Method == "GET" {
			assert.Contains(req.URL.Path, checkSuite.CheckSuite.HeadSha, description)
			response = payloads.MultipleCheckSuiteResponse
			response = []byte(strings.Replace(string(response),
				`"conclusion": "neutral"`, fmt.Sprintf("\"conclusion\": \"%s\"", state1), 1))
			if state2 != "" {
				response = []byte(strings.Replace(string(response),
					`"conclusion": "neutral"`, fmt.Sprintf("\"conclusion\": \"%s\"", state2), 1))
			}
		} else if strings.Contains(checkSuite.GetStatusesUrl(), req.URL.String()) && req.Method == "POST" {
			assert.Contains(req.URL.Path, checkSuite.CheckSuite.HeadSha)
			status := getStatusBody(assert, req)
			*postedState = status.State
			*postedStatus = true
			response = payloads.StatusResponse
		} else {
			assert.Fail("%s: Unexpected %s request to '%s'", description, req.Method, req.URL.String())
		}

		w.Write(response)
	})

	return httptest.NewServer(fn)
}

func TestCheckSuite(t *testing.T) {
	assert := assert.New(t)
	payloads, err := getPayloads()
	assert.NoError(err)
	var zeroCommitState CommitState
	singleAppTarget := []string{"octocoders-linter"}
	multiAppTarget := []string{"Octocat App", "Hexacat App"}
	noMatchAppTarget := []string{"no-match"}
	servers := []*httptest.Server{}

	for i, tc := range []TestCheckSuiteCase{
		{"POST pending for single suite unfinished", singleAppTarget, "", "", true, CommitStatePending,
			[]byte(strings.ReplaceAll(string(payloads.CheckSuiteEvent), `"conclusion": "success"`, fmt.Sprintf("\"conclusion\": \"%s\"", CommitStatePending)))},
		{"POST pending for single suite failure", singleAppTarget, "", "", true, CommitStatePending,
			[]byte(strings.ReplaceAll(string(payloads.CheckSuiteEvent), `"conclusion": "success"`, fmt.Sprintf("\"conclusion\": \"%s\"", CommitStateFailure)))},
		{"POST success for single suite", singleAppTarget, "", "", true, CommitStateSuccess, payloads.CheckSuiteEvent},
		{"POST pending for no match, single suite", noMatchAppTarget, "", "", true, CommitStatePending, payloads.CheckSuiteEvent},
		{"POST pending for multiple suites pending", multiAppTarget, CommitStateSuccess, CommitStatePending, true, CommitStatePending, payloads.CheckSuiteEvent},
		{"POST pending for multiple suites pending 2", multiAppTarget, CommitStatePending, CommitStateSuccess, true, CommitStatePending, payloads.CheckSuiteEvent},
		{"POST pending for multiple suites failure", multiAppTarget, CommitStateSuccess, CommitStateFailure, true, CommitStatePending, payloads.CheckSuiteEvent},
		{"POST success for multiple suites", multiAppTarget, CommitStateSuccess, CommitStateSuccess, true, CommitStateSuccess, payloads.CheckSuiteEvent},
		{"skip for main branch", singleAppTarget, CommitStateSuccess, "", false, zeroCommitState, []byte(strings.ReplaceAll(string(payloads.CheckSuiteEvent), `"head_branch": "changes"`, `"head_branch": "main"`))},
	} {
		var postedStatus bool
		var postedState CommitState
		server := NewCheckSuiteTestServer(assert, payloads, tc.InjectState1, tc.InjectState2, &postedState, &postedStatus, tc.Description)
		gh, err := NewGithubClient(server.URL, "", tc.AppTargets...)
		assert.NoError(err, tc.Description)
		servers = append(servers, server)
		defer servers[i].Close()
		fmt.Println(fmt.Sprintf("\n\n========= %s =========", tc.Description))
		err = handleEvent(gh, tc.Event)
		assert.NoError(err, tc.Description)
		assert.Equal(tc.ShouldPostStatus, postedStatus, tc.Description)
		assert.Equal(tc.ExpectedStatus, postedState, tc.Description)
	}
}

type TestCommentCase struct {
	Description       string
	InputComment      string
	InjectConclusion  CheckSuiteConclusion
	ExpectedState     CommitState
	ShouldPostStatus  bool
	ShouldPostComment bool
	ExpectedComment   string
	AppTargets        []string
}

func NewCommentTestServer(
	assert *assert.Assertions,
	payloads Payloads,
	inputComment string,
	injectConclusion CheckSuiteConclusion,
	expectedState CommitState,
	postedStatus *bool,
	postedComment *bool,
	expectedComment string,
	description string,
) *httptest.Server {
	issueCommentEvent := NewIssueCommentWebhook(payloads.IssueCommentEvent)
	assert.NotEmpty(issueCommentEvent)
	pullRequestResponse := NewPullRequest(payloads.PullRequestResponse)
	assert.NotEmpty(pullRequestResponse)

	fn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response := []byte{}
		if strings.Contains(issueCommentEvent.GetPullsUrl(), req.URL.String()) && req.Method == "GET" {
			response = payloads.PullRequestResponse
		} else if strings.Contains(pullRequestResponse.GetCheckSuiteUrl(), req.URL.String()) && req.Method == "GET" {
			response = []byte(strings.ReplaceAll(
				string(payloads.CheckSuiteResponse),
				`"conclusion": "neutral"`,
				fmt.Sprintf("\"conclusion\": \"%s\"", injectConclusion)))
		} else if strings.Contains(pullRequestResponse.StatusesUrl, req.URL.String()) && req.Method == "POST" {
			*postedStatus = true
			response = payloads.StatusResponse
			status := getStatusBody(assert, req)
			assert.Equal(expectedState, status.State, description)
		} else if strings.Contains(issueCommentEvent.GetCommentsUrl(), req.URL.String()) && req.Method == "POST" {
			*postedComment = true
			response = payloads.NewCommentResponse
			body, err := ioutil.ReadAll(req.Body)
			assert.NoError(err, description)
			assert.Equal(expectedComment, string(body), "%s: Comment body for command '%s'", description, inputComment)
		} else {
			assert.Fail("%s: Unexpected %s request to '%s'", description, req.Method, req.URL.String())
		}

		w.Write(response)
	})

	return httptest.NewServer(fn)
}

func TestComments(t *testing.T) {
	assert := assert.New(t)
	payloads, err := getPayloads()
	assert.NoError(err)
	issueCommentEvent := NewIssueCommentWebhook(payloads.IssueCommentEvent)
	assert.NotEmpty(issueCommentEvent)
	pullRequestResponse := NewPullRequest(payloads.PullRequestResponse)
	assert.NotEmpty(pullRequestResponse)
	noPipelinesComment, err := NewIssueCommentBody(string(payloads.NoPipelinesComment))
	assert.NoError(err)
	helpComment, err := NewIssueCommentBody(string(payloads.HelpComment))
	assert.NoError(err)

	servers := []*httptest.Server{}
	apps := []string{"Octocat App"}
	noMatchAppTarget := []string{"no-match"}

	cases := []TestCommentCase{
		{"override+success", "/check-enforcer override", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false, "", apps},
		{"override+failure", "/check-enforcer override", CheckSuiteConclusionFailure, CommitStateSuccess, true, false, "", apps},
		{"comment spaces", "   /check-enforcer   override   ", CheckSuiteConclusionFailure, CommitStateSuccess, true, false, "", apps},
		{"reset+success", "/check-enforcer reset", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false, "", apps},
		{"reset+failure", "/check-enforcer reset", CheckSuiteConclusionFailure, CommitStatePending, true, false, "", apps},
		{"evaluate+success", "/check-enforcer evaluate", CheckSuiteConclusionSuccess, CommitStateSuccess, true, false, "", apps},
		{"evaluate+failure", "/check-enforcer evaluate", CheckSuiteConclusionFailure, CommitStatePending, true, false, "", apps},
		{"evaluate+timeout", "/check-enforcer evaluate", CheckSuiteConclusionTimedOut, CommitStatePending, true, false, "", apps},
		{"evaluate+neutral", "/check-enforcer evaluate", CheckSuiteConclusionNeutral, CommitStatePending, true, false, "", apps},
		{"evaluate+stale", "/check-enforcer evaluate", CheckSuiteConclusionStale, CommitStatePending, true, false, "", apps},
		{"evaluate+nopipelinematches", "/check-enforcer evaluate", CheckSuiteConclusionSuccess, CommitStatePending, true, true, string(noPipelinesComment), noMatchAppTarget},
		{"reset+nopipelinematches", "/check-enforcer reset", CheckSuiteConclusionSuccess, CommitStatePending, true, true, string(noPipelinesComment), noMatchAppTarget},
		{"help", "/check-enforcer help", "", "", false, true, string(helpComment), apps},
		{"missing space", "/check-enforcerevaluate", "", "", false, true, string(helpComment), apps},
		{"invalid command", "/check-enforcer foobar", "", "", false, true, string(helpComment), apps},
		{"invalid command+args", "/check-enforcer foobar bar bar", "", "", false, true, string(helpComment), apps},
		{"different command", "/azp run", "", "", false, false, "", apps},
		{"semicolons", ";;;;;;;;;;;;;;;;;;;;;;;;;;;", "", "", false, false, "", apps},
	}

	for i, tc := range cases {
		var postedStatus bool
		var postedComment bool

		server := NewCommentTestServer(assert, payloads, tc.InputComment, tc.InjectConclusion, tc.ExpectedState,
			&postedStatus, &postedComment, tc.ExpectedComment, tc.Description)
		servers = append(servers, server)
		defer servers[i].Close()

		gh, err := NewGithubClient(server.URL, "", tc.AppTargets...)
		assert.NoError(err, tc.Description)

		replaced := strings.ReplaceAll(string(payloads.IssueCommentEvent), "You are totally right! I'll get this fixed right away.", tc.InputComment)

		err = handleEvent(gh, []byte(replaced))
		assert.NoError(err)
		assert.Equal(tc.ShouldPostStatus, postedStatus, "%s: Should POST status for command '%s'", tc.Description, tc.InputComment)
		assert.Equal(tc.ShouldPostComment, postedComment, "%s: Should POST comment for command '%s'", tc.Description, tc.InputComment)
	}
}

type WorkflowRunCase struct {
	Description        string
	Event              []byte
	CheckSuiteResponse []byte
	ExpectedState      CommitState
}

func NewWorkflowRunTestServer(
	assert *assert.Assertions,
	payloads Payloads,
	checkSuiteResponse []byte,
	postedState *CommitState,
	description string,
) *httptest.Server {
	workflowRun := NewWorkflowRunWebhook(payloads.WorkflowRunEvent)
	assert.NotEmpty(workflowRun)

	fn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response := []byte{}

		if strings.Contains(workflowRun.GetCheckSuiteUrl(), req.URL.String()) && req.Method == "GET" {
			response = checkSuiteResponse
		} else if strings.Contains(workflowRun.GetStatusesUrl(), req.URL.String()) && req.Method == "POST" {
			assert.Contains(req.URL.Path, workflowRun.HeadSha)
			status := getStatusBody(assert, req)
			*postedState = status.State
			response = payloads.StatusResponse
		} else {
			assert.Fail("%s: Unexpected %s request to '%s'", description, req.Method, req.URL.String())
		}

		w.Write(response)
	})

	return httptest.NewServer(fn)
}

func TestWorkflowRun(t *testing.T) {
	assert := assert.New(t)
	payloads, err := getPayloads()
	assert.NoError(err)
	servers := []*httptest.Server{}

	singleCheckSuiteResponse := []byte(strings.ReplaceAll(
		string(payloads.CheckSuiteResponse), `"conclusion": "neutral"`, fmt.Sprintf("\"conclusion\": \"%s\"", CommitStateSuccess)))

	multipleCheckSuiteResponse := []byte(strings.ReplaceAll(string(payloads.MultipleCheckSuiteResponse),
		`"conclusion": "neutral"`, fmt.Sprintf("\"conclusion\": \"%s\"", CommitStateSuccess)))
	multipleCheckSuiteResponsePending := []byte(strings.Replace(string(multipleCheckSuiteResponse),
		fmt.Sprintf("\"conclusion\": \"%s\"", CommitStateSuccess), fmt.Sprintf("\"conclusion\": \"%s\"", CommitStatePending), 1))

	for i, tc := range []WorkflowRunCase{
		{"Workflow run success", payloads.WorkflowRunEvent, singleCheckSuiteResponse, CommitStateSuccess},
		{"Workflow run multiple success", payloads.WorkflowRunEvent, multipleCheckSuiteResponse, CommitStateSuccess},
		{"Workflow run multiple pending", payloads.WorkflowRunEvent, multipleCheckSuiteResponsePending, CommitStatePending},
		{"Workflow run push event", []byte(strings.ReplaceAll(string(payloads.WorkflowRunEvent), `"event": "pull_request"`, `"event": "push"`)),
			singleCheckSuiteResponse, CommitStatePending},
	} {
		var postedState CommitState

		server := NewWorkflowRunTestServer(assert, payloads, tc.CheckSuiteResponse, &postedState, tc.Description)
		gh, err := NewGithubClient(server.URL, "", "Octocat App")
		assert.NoError(err)
		servers = append(servers, server)
		defer servers[i].Close()

		err = handleEvent(gh, tc.Event)
		assert.NoError(err)

		assert.Equal(tc.ExpectedState, postedState, tc.Description)
	}
}
