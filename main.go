package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"unicode"
)

const GithubTokenKey = "GITHUB_TOKEN"
const CommitStatusContext = "https://aka.ms/azsdk/checkenforcer"
const AzurePipelinesAppName = "Azure Pipelines"

var pendingBody = StatusBody{
	State:       CommitStatePending,
	Description: "Waiting for all checks to complete",
	Context:     CommitStatusContext,
}

var succeededBody = StatusBody{
	State:       CommitStateSuccess,
	Description: "All checks passed",
	Context:     CommitStatusContext,
}

var failedBody = StatusBody{
	State:       CommitStateFailure,
	Description: "Some checks failed",
	Context:     CommitStatusContext,
}

func main() {
	if len(os.Args) <= 1 {
		help()
		os.Exit(1)
	}

	payloadPath := os.Args[1]
	payload, err := ioutil.ReadFile(payloadPath)
	handleError(err)

	github_token := os.Getenv(GithubTokenKey)
	if github_token == "" {
		fmt.Println(fmt.Sprintf("WARNING: environment variable '%s' is not set", GithubTokenKey))
	}

	gh, err := NewGithubClient("https://api.github.com", github_token, AzurePipelinesAppName)
	handleError(err)

	err = handleEvent(gh, payload)
	handleError(err)
}

func handleEvent(gh *GithubClient, payload []byte) error {
	fmt.Println("Handling Event. Payload:")
	fmt.Println(string(payload))

	if ic := NewIssueCommentWebhook(payload); ic != nil {
		fmt.Println("Handling issue comment event.")
		err := handleComment(gh, ic)
		handleError(err)
		return nil
	}

	if cs := NewCheckSuiteWebhook(payload); cs != nil {
		fmt.Println("Handling check suite event.")
		err := handleCheckSuite(gh, cs)
		handleError(err)
		return nil
	}

	return errors.New("Error: Invalid or unsupported payload body.")
}

func handleError(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func sanitizeComment(comment string) string {
	result := []rune{}
	comment = strings.TrimSpace(comment)
	for _, r := range comment {
		if unicode.IsLetter(r) || unicode.IsSpace(r) || r == '/' || r == '-' {
			result = append(result, unicode.ToLower(r))
		}
	}
	return string(result)
}

func getCheckEnforcerCommand(comment string) string {
	comment = sanitizeComment(comment)
	baseCommand := "/check-enforcer"

	if !strings.HasPrefix(comment, baseCommand) {
		fmt.Println(fmt.Sprintf("Skipping comment that does not start with '%s'", baseCommand))
		return ""
	}

	re := regexp.MustCompile(`\s*` + baseCommand + `\s*([A-z]*)`)
	matches := re.FindStringSubmatch(comment)
	if len(matches) >= 1 {
		command := matches[1]
		if command == "override" || command == "evaluate" || command == "reset" {
			fmt.Println("Parsed check enforcer command", command)
			return command
		}
		fmt.Println("Supported commands are 'override', 'evaluate', or 'reset' but found:", command)
		return command
	} else {
		fmt.Println("Command does not match format '/check-enforcer [override|reset|evaluate]'")
		return ""
	}
}

func handleComment(gh *GithubClient, ic *IssueCommentWebhook) error {
	command := getCheckEnforcerCommand(ic.Comment.Body)

	if command == "" {
		return nil
	} else if command == "override" {
		pr, err := gh.GetPullRequest(ic.GetPullsUrl())
		handleError(err)
		return gh.SetStatus(pr.GetStatusesUrl(), succeededBody)
	} else if command == "evaluate" {
		// We cannot use the commits url from the issue object because it
		// is targeted to the main repo. To get all check suites for a commit,
		// a request must be made to the repos API for the repository the pull
		// request branch is from, which may be a fork.
		pr, err := gh.GetPullRequest(ic.GetPullsUrl())
		handleError(err)
		_, conclusion, err := gh.GetCheckSuiteStatus(pr)
		handleError(err)

		if IsCheckSuiteSucceeded(conclusion) {
			return gh.SetStatus(pr.GetStatusesUrl(), succeededBody)
		} else if IsCheckSuiteFailed(conclusion) {
			return gh.SetStatus(pr.GetStatusesUrl(), failedBody)
		} else {
			return gh.SetStatus(pr.GetStatusesUrl(), pendingBody)
		}
	} else if command == "reset" {
		pr, err := gh.GetPullRequest(ic.GetPullsUrl())
		handleError(err)
		return gh.SetStatus(pr.GetStatusesUrl(), pendingBody)
	} else {
		return nil
	}
}

func handleCheckSuite(gh *GithubClient, cs *CheckSuiteWebhook) error {
	if cs.CheckSuite.App.Name != gh.AppTarget {
		fmt.Println(fmt.Sprintf(
			"Check Enforcer only handles check suites from the '%s' app. Found: '%s'",
			gh.AppTarget,
			cs.CheckSuite.App.Name))
		return nil
	} else if cs.CheckSuite.HeadBranch == "main" {
		fmt.Println("Skipping check suite for main branch.")
		return nil
	} else if cs.IsSucceeded() {
		body := succeededBody
		body.TargetUrl = cs.CheckSuite.Url
		return gh.SetStatus(cs.GetStatusesUrl(), body)
	} else if cs.IsFailed() {
		body := failedBody
		body.TargetUrl = cs.CheckSuite.Url
		return gh.SetStatus(cs.GetStatusesUrl(), body)
	} else {
		fmt.Println("Skipping check suite with conclusion: ", cs.CheckSuite.Conclusion)
		return nil
	}
}

func help() {
	help := `Update pull request status checks based on github webhook events.

USAGE
  go run main.go <payload json file>

BEHAVIORS
  complete:
    Sets the check enforcer status for a commit to the value of the check_suite status
    Handles payload type: https://docs.github.com/en/developers/webhooks-and-events/webhooks/webhook-events-and-payloads#check_suite`

	fmt.Println(help)
}
