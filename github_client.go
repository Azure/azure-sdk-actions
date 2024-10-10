package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	fmt.Println("POST to", target.String(), "with state", status.State)
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

	fmt.Println("GET to", target.String())
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

	fmt.Println("GET to", target.String())
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

	fmt.Println("POST to", target.String())
	_, err = gh.request(req)
	return err
}

func (gh *GithubClient) request(req *http.Request) ([]byte, error) {
	resp, err := gh.client.Do(req)
	if err != nil {
		return []byte{}, err
	}

	defer resp.Body.Close()
	fmt.Println("Received", resp.Status)
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
