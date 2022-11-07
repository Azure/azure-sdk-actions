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
	client    *http.Client
	token     string
	BaseUrl   url.URL
	AppTarget string
}

func NewGithubClient(baseUrl string, token string, appTarget string) (*GithubClient, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}
	return &GithubClient{
		client:    &http.Client{},
		BaseUrl:   *u,
		token:     token,
		AppTarget: appTarget,
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

func (gh *GithubClient) GetCheckSuiteStatus(pr PullRequest) (CheckSuiteStatus, CheckSuiteConclusion, error) {
	csUrl := pr.GetCheckSuiteUrl()

	target, err := gh.getUrl(csUrl)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequest("GET", target.String(), nil)
	if err != nil {
		return "", "", err
	}

	gh.setHeaders(req)

	fmt.Println("GET to", target.String())
	data, err := gh.request(req)
	if err != nil {
		return "", "", err
	}
	suites := CheckSuites{}
	if err = json.Unmarshal(data, &suites); err != nil {
		return "", "", err
	}

	for _, cs := range suites.CheckSuites {
		if cs.App.Name != gh.AppTarget {
			continue
		}
		return cs.Status, cs.Conclusion, nil
	}

	return "", "", nil
}

func (gh *GithubClient) CreateIssueComment(commentsUrl string, body string) error {
	target, err := gh.getUrl(commentsUrl)
	if err != nil {
		return nil
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
		return nil
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
	fmt.Println("Response:")
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, err
	}
	fmt.Println(fmt.Sprintf("%s", data))

	if resp.StatusCode >= 400 {
		return []byte{}, errors.New(fmt.Sprintf("Received http error %d", resp.StatusCode))
	}

	return data, nil
}
