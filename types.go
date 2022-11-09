package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	CommitStatePending CommitState = "pending"
	CommitStateSuccess CommitState = "success"
	CommitStateFailure CommitState = "failure"
	CommitStateError   CommitState = "error"

	CheckSuiteActionCompleted ActionType = "completed"

	IssueCommentActionCreated ActionType = "created"

	CheckSuiteStatusRequested  CheckSuiteStatus = "requested"
	CheckSuiteStatusInProgress CheckSuiteStatus = "in_progress"
	CheckSuiteStatusCompleted  CheckSuiteStatus = "completed"

	CheckSuiteConclusionSuccess        CheckSuiteConclusion = "success"
	CheckSuiteConclusionFailure        CheckSuiteConclusion = "failure"
	CheckSuiteConclusionNeutral        CheckSuiteConclusion = "neutral"
	CheckSuiteConclusionCancelled      CheckSuiteConclusion = "cancelled"
	CheckSuiteConclusionTimedOut       CheckSuiteConclusion = "timed_out"
	CheckSuiteConclusionActionRequired CheckSuiteConclusion = "action_required"
	CheckSuiteConclusionStale          CheckSuiteConclusion = "stale"
)

type ActionType string
type CommitState string
type CheckSuiteStatus string
type CheckSuiteConclusion string

type StatusBody struct {
	State       CommitState `json:"state"`
	Description string      `json:"description"`
	Context     string      `json:"context"`
	TargetUrl   string      `json:"target_url"`
}

type PullRequest struct {
	Url         string `json:"url"`
	HtmlUrl     string `json:"html_url"`
	Id          int    `json:"id"`
	Number      int    `json:"number"`
	State       string `json:"state"`
	Title       string `json:"title"`
	StatusesUrl string `json:"statuses_url"`
	Head        struct {
		Sha  string `json:"sha"`
		Repo Repo   `json:"repo"`
	} `json:"head"`
	Base struct {
		Repo Repo `json:"repo"`
	} `json:"base"`
}

type Repo struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Url         string `json:"url"`
	CommitsUrl  string `json:"commits_url"`
	HtmlUrl     string `json:"html_url"`
	IssuesUrl   string `json:"issues_url"`
	PullsUrl    string `json:"pulls_url"`
	StatusesUrl string `json:"statuses_url"`
}

type CheckSuites struct {
	Count       int          `json:"total_count"`
	CheckSuites []CheckSuite `json:"check_suites"`
}

type CheckSuite struct {
	Id           int                  `json:"id"`
	HeadBranch   string               `json:"head_branch"`
	HeadSha      string               `json:"head_sha"`
	Status       CheckSuiteStatus     `json:"status"`
	Conclusion   CheckSuiteConclusion `json:"conclusion"`
	Url          string               `json:"url"`
	CheckRunsUrl string               `json:"check_runs_url"`
	App          App                  `json:"app"`
}

type App struct {
	Name string `json:"name"`
}

type Issue struct {
	Url         string `json:"url"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	CommentsUrl string `json:"comments_url"`
}

type IssueComment struct {
	Url     string `json:"url"`
	HtmlUrl string `json:"html_url"`
	Id      int    `json:"id"`
	Body    string `json:"body"`
}

type IssueCommentBody struct {
	Body string `json:"body"`
}

type CheckSuiteWebhook struct {
	Action     ActionType `json:"action"`
	CheckSuite CheckSuite `json:"check_suite"`
	Repo       Repo       `json:"repository"`
}

func IsCheckSuiteNoMatch(conclusion CheckSuiteConclusion) bool {
	return conclusion == ""
}

func IsCheckSuiteSucceeded(conclusion CheckSuiteConclusion) bool {
	return conclusion == CheckSuiteConclusionSuccess
}

func IsCheckSuiteFailed(conclusion CheckSuiteConclusion) bool {
	return conclusion == CheckSuiteConclusionFailure || conclusion == CheckSuiteConclusionTimedOut
}

func (cs *CheckSuite) IsSucceeded() bool {
	return IsCheckSuiteSucceeded(cs.Conclusion)
}

func (cs *CheckSuite) IsFailed() bool {
	return IsCheckSuiteFailed(cs.Conclusion)
}

func (csw *CheckSuiteWebhook) IsSucceeded() bool {
	return csw.CheckSuite.IsSucceeded()
}

func (csw *CheckSuiteWebhook) IsFailed() bool {
	return csw.CheckSuite.IsFailed()
}

func (csw *CheckSuiteWebhook) GetStatusesUrl() string {
	return strings.ReplaceAll(csw.Repo.StatusesUrl, "{sha}", csw.CheckSuite.HeadSha)
}

type IssueCommentWebhook struct {
	Action  ActionType   `json:"action"`
	Issue   Issue        `json:"issue"`
	Comment IssueComment `json:"comment"`
	Repo    Repo         `json:"repository"`
}

func (pr *PullRequest) GetCheckSuiteUrl() string {
	return strings.ReplaceAll(pr.Head.Repo.CommitsUrl, "{/sha}", fmt.Sprintf("/%s", pr.Head.Sha)) + "/check-suites"
}

func (ic *IssueCommentWebhook) GetPullsUrl() string {
	return strings.ReplaceAll(ic.Repo.PullsUrl, "{/number}", fmt.Sprintf("/%d", ic.Issue.Number))
}

func (ic *IssueCommentWebhook) GetCommentsUrl() string {
	return ic.Issue.CommentsUrl
}

func NewIssueCommentBody(body string) ([]byte, error) {
	jsonBody, err := json.Marshal(IssueCommentBody{body})
	if err != nil {
		return nil, err
	}
	return jsonBody, nil
}

func NewPullRequest(payload []byte) *PullRequest {
	var pr PullRequest
	if err := json.Unmarshal(payload, &pr); err != nil {
		return nil
	}
	if pr.Url == "" && pr.Number == 0 {
		return nil
	}
	return &pr
}

func NewCheckSuiteWebhook(payload []byte) *CheckSuiteWebhook {
	var cs CheckSuiteWebhook
	if err := json.Unmarshal(payload, &cs); err != nil {
		return nil
	}
	if cs.CheckSuite.Id == 0 {
		return nil
	}
	return &cs
}

func NewIssueCommentWebhook(payload []byte) *IssueCommentWebhook {
	var ic IssueCommentWebhook
	if err := json.Unmarshal(payload, &ic); err != nil {
		return nil
	}
	if ic.Issue.Number == 0 {
		return nil
	}

	return &ic
}
