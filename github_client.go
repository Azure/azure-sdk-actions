package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type GithubClient struct {
	client     *http.Client
	token      string
	BaseUrl    url.URL
	AppTargets []string
}

func NewGithubClient(baseUrl string, token string, appTargets ...string) (*GithubClient, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}
	return &GithubClient{
		client:     &http.Client{},
		BaseUrl:    *u,
		token:      token,
		AppTargets: appTargets,
	}, nil
}

func (gh *GithubClient) setHeaders(req *http.Request) {
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	req.Header.Add("Authorization", fmt.Sprintf("token %s", gh.token))
}

func (gh *GithubClient) getUrl(target string) (*url.URL, error) {
	targetUrl, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	targetUrl.Scheme = gh.BaseUrl.Scheme
	targetUrl.Host = gh.BaseUrl.Host
	return targetUrl, nil
}

func (gh *GithubClient) SetStatus(statusUrl string, status StatusBody) error {
	body, err := json.Marshal(status)
	if err != nil {
		return err
	}

	target, err := gh.getUrl(statusUrl)
	if err != nil {
		return err
	}

	reader := bytes.NewReader(body)

	req, err := http.NewRequest("POST", target.String(), reader)
	if err != nil {
		return err
	}

	gh.setHeaders(req)

	_, err = gh.request(req)
	if err != nil {
		return err
	}

	return nil
}

func (gh *GithubClient) GetPullRequest(pullsUrl string) (PullRequest, error) {
	target, err := gh.getUrl(pullsUrl)
	if err != nil {
		return PullRequest{}, err
	}

	req, err := http.NewRequest("GET", target.String(), nil)
	if err != nil {
		return PullRequest{}, err
	}

	gh.setHeaders(req)

	data, err := gh.request(req)
	if err != nil {
		return PullRequest{}, err
	}

	pr := PullRequest{}
	if err = json.Unmarshal(data, &pr); err != nil {
		return PullRequest{}, err
	}

	return pr, nil
}

func (gh *GithubClient) FilterCheckSuiteStatuses(checkSuites []CheckSuite) []CheckSuite {
	filteredCheckSuites := []CheckSuite{}

	for _, cs := range checkSuites {
		for _, target := range gh.AppTargets {
			// Ignore auxiliary checks we don't control, e.g. Microsoft Policy Service.
			// Github creates a check suite for each app with checks:write permissions,
			// so also ignore any check suites with 0 check runs posted
			//
			// TODO: in the case where a check run isn't posted from azure pipelines due to invalid yaml, will this
			// show up as 0 check runs? If so, how do we differentiate between the following so we don't submit a passing status:
			//    1. Github Actions CI intended, Azure Pipelines CI NOT detected
			//    2. Github Actions CI intended, Azure Pipelines CI intended, Azure Pipelines CI invalid yaml
			if cs.App.Name == target && cs.LatestCheckRunCount > 0 {
				filteredCheckSuites = append(filteredCheckSuites, cs)
			}
		}
	}

	return filteredCheckSuites
}

func (gh *GithubClient) GetCheckSuiteStatuses(checkSuiteUrl string) ([]CheckSuite, error) {
	target, err := gh.getUrl(checkSuiteUrl)
	if err != nil {
		return []CheckSuite{}, err
	}

	req, err := http.NewRequest("GET", target.String(), nil)
	if err != nil {
		return []CheckSuite{}, err
	}

	gh.setHeaders(req)

	data, err := gh.request(req)
	if err != nil {
		return []CheckSuite{}, err
	}
	suites := CheckSuites{}
	if err = json.Unmarshal(data, &suites); err != nil {
		return []CheckSuite{}, err
	}

	return gh.FilterCheckSuiteStatuses(suites.CheckSuites), nil
}

func (gh *GithubClient) CreateIssueComment(commentsUrl string, body string) error {
	target, err := gh.getUrl(commentsUrl)
	if err != nil {
		return err
	}

	fmt.Println("Creating new issue comment with contents:")
	fmt.Println("=====================================")
	fmt.Println(body)
	fmt.Println("=====================================")

	reqBody, err := NewIssueCommentBody(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", target.String(), bytes.NewReader(reqBody))
	if err != nil {
		return err
	}

	gh.setHeaders(req)

	_, err = gh.request(req)
	return err
}

func (gh *GithubClient) request(req *http.Request) ([]byte, error) {
	gh.logRequest(req)

	resp, err := gh.client.Do(req)
	if err != nil {
		return []byte{}, err
	}

	defer resp.Body.Close()
	gh.logRateLimit(resp)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, err
	}

	if resp.StatusCode >= 400 {
		fmt.Println("Error Response:")
		fmt.Println(fmt.Sprintf("%s", data))
		return []byte{}, errors.New(fmt.Sprintf("Received http error %d", resp.StatusCode))
	}

	return data, nil
}

// logRequest logs the outgoing API call in the format:
//
//	[github] GET https://api.github.com/... {"body":"..."}
func (gh *GithubClient) logRequest(req *http.Request) {
	body := ""
	if req.GetBody != nil {
		if reader, err := req.GetBody(); err == nil {
			defer reader.Close()
			if data, err := io.ReadAll(reader); err == nil {
				body = string(data)
			}
		}
	}

	fmt.Println(fmt.Sprintf("[github] %s %s %s", req.Method, req.URL.String(), body))
}

// logRateLimit extracts the rate limit headers from a response and logs them,
// prefixed by the response status, in the format:
//
//	[github] status: 201, load: 1%, used: 105, remaining: 14895, reset: 00:19:31
//
// load is the ratio of used requests to the requests that should have been
// available by now if usage were spread evenly across the rate limit window. A
// load over 100% means we are predicted to hit the limit before it resets.
func (gh *GithubClient) logRateLimit(resp *http.Response) {
	status := fmt.Sprintf("status: %d", resp.StatusCode)

	limitHeader := resp.Header.Get("x-ratelimit-limit")
	remainingHeader := resp.Header.Get("x-ratelimit-remaining")
	resetHeader := resp.Header.Get("x-ratelimit-reset")

	if limitHeader == "" || remainingHeader == "" || resetHeader == "" {
		fmt.Printf("[github] %s, missing ratelimit header(s) in response\n", status)
		return
	}

	limit, err := strconv.Atoi(limitHeader)
	if err != nil {
		fmt.Printf("[github] %s, invalid x-ratelimit-limit header: %s\n", status, limitHeader)
		return
	}
	remaining, err := strconv.Atoi(remainingHeader)
	if err != nil {
		fmt.Printf("[github] %s, invalid x-ratelimit-remaining header: %s\n", status, remainingHeader)
		return
	}
	resetUnix, err := strconv.ParseInt(resetHeader, 10, 64)
	if err != nil {
		fmt.Printf("[github] %s, invalid x-ratelimit-reset header: %s\n", status, resetHeader)
		return
	}

	used := limit - remaining

	now := time.Now()
	reset := time.Unix(resetUnix, 0)
	// The rate limit window is one hour, so it started one hour before reset.
	start := reset.Add(-time.Hour)
	elapsedFraction := now.Sub(start).Seconds() / time.Hour.Seconds()

	// Example: If limit is 1000, and 6 minutes have elapsed (10% of 1 hour),
	// availableLimit will be 100 (10% of total).
	availableLimit := float64(limit) * elapsedFraction

	// If load is > 100%, we are "running hot" and predicted to hit the limit
	// before reset. Keep load < 50% for a safety margin.
	load := float64(used) / availableLimit

	fmt.Println(fmt.Sprintf("[github] %s, load: %s, used: %d, remaining: %d, reset: %s",
		status, toPercent(load), used, remaining, formatDuration(getDuration(now, reset))))
}
