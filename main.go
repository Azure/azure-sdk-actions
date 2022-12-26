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
const GithubActionsAppName = "Github Actions"

func newPendingBody() StatusBody {
	return StatusBody{
		State:       CommitStatePending,
		Description: "Waiting for all checks to succeed",
		Context:     CommitStatusContext,
		TargetUrl:   getActionLink(),
	}
}

func newSucceededBody() StatusBody {
	return StatusBody{
		State:       CommitStateSuccess,
		Description: "All checks passed",
		Context:     CommitStatusContext,
		TargetUrl:   getActionLink(),
	}
}

// NOTE: This is currently unused as we post a pending state on check_suite failure,
// but keep the function around for now in case we want to revert this behavior.
func newFailedBody() StatusBody {
	return StatusBody{
		State:       CommitStateFailure,
		Description: "Some checks failed",
		Context:     CommitStatusContext,
		TargetUrl:   getActionLink(),
	}
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

	gh, err := NewGithubClient("https://api.github.com", github_token, AzurePipelinesAppName, GithubActionsAppName)
	handleError(err)

	err = handleEvent(gh, payload)
	handleError(err)
}

func handleEvent(gh *GithubClient, payload []byte) error {
	fmt.Println("################################################")
	fmt.Println("#  AZURE SDK CHECK ENFORCER                    #")
	fmt.Println("#  Docs: https://aka.ms/azsdk/checkenforcer    #")
	fmt.Println("################################################")
	fmt.Println()

	if ic := NewIssueCommentWebhook(payload); ic != nil {
		err := handleIssueComment(gh, ic)
		handleError(err)
		return nil
	}

	if cs := NewCheckSuiteWebhook(payload); cs != nil {
		err := handleCheckSuite(gh, cs)
		handleError(err)
		return nil
	}

	if wr := NewWorkflowRunWebhook(payload); wr != nil {
		err := handleWorkflowRun(gh, wr)
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

func getActionLink() string {
	// In a github actions environment these env variables will be set.
	// https://docs.github.com/en/actions/learn-github-actions/environment-variables#default-environment-variables
	if runId, ok := os.LookupEnv("GITHUB_RUN_ID"); ok {
		repo := os.Getenv("GITHUB_REPOSITORY")
		server := os.Getenv("GITHUB_SERVER_URL")
		return fmt.Sprintf("%s/%s/actions/runs/%s", server, repo, runId)
	}
	return CommitStatusContext
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

	re := regexp.MustCompile(`\s*` + baseCommand + `\s+([A-z]*)`)
	matches := re.FindStringSubmatch(comment)
	if len(matches) >= 1 {
		command := matches[1]
		if command == "override" || command == "evaluate" || command == "reset" || command == "help" {
			fmt.Println("Parsed check enforcer command", command)
			return command
		}
		fmt.Println("Supported commands are 'override', 'evaluate', 'reset', or 'help' but found:", command)
		return command
	} else {
		fmt.Println("Command does not match format '/check-enforcer [override|reset|evaluate|help]'")
		return "UNKNOWN"
	}
}

func handleIssueComment(gh *GithubClient, ic *IssueCommentWebhook) error {
	fmt.Println("Handling issue comment event.")

	command := getCheckEnforcerCommand(ic.Comment.Body)

	if command == "" {
		return nil
	} else if command == "override" {
		pr, err := gh.GetPullRequest(ic.GetPullsUrl())
		handleError(err)
		return gh.SetStatus(pr.StatusesUrl, newSucceededBody())
	} else if command == "evaluate" || command == "reset" {
		// We cannot use the commits url from the issue object because it
		// is targeted to the main repo. To get all check suites for a commit,
		// a request must be made to the repos API for the repository the pull
		// request branch is from, which may be a fork.
		pr, err := gh.GetPullRequest(ic.GetPullsUrl())
		handleError(err)
		checkSuites, err := gh.GetCheckSuiteStatuses(pr.GetCheckSuiteUrl())
		handleError(err)

		if checkSuites == nil || len(checkSuites) == 0 {
			noPipelineText, err := ioutil.ReadFile("./comments/no_pipelines.txt")
			handleError(err)
			err = gh.CreateIssueComment(ic.GetCommentsUrl(), string(noPipelineText))
			handleError(err)
		}

		return handleCheckSuiteConclusions(gh, checkSuites, pr.StatusesUrl)
	} else {
		helpText, err := ioutil.ReadFile("./comments/help.txt")
		handleError(err)
		err = gh.CreateIssueComment(ic.GetCommentsUrl(), string(helpText))
		handleError(err)
	}

	return nil
}

func handleCheckSuiteConclusions(gh *GithubClient, checkSuites []CheckSuite, statusesUrl string) error {
	successCount := 0

	for _, suite := range checkSuites {
		fmt.Println(fmt.Sprintf("Check suite conclusion for '%s' is '%s'.", suite.App.Name, suite.Conclusion))
		if IsCheckSuiteSucceeded(suite.Conclusion) {
			successCount++
		}
	}

	if successCount > 0 && successCount == len(checkSuites) {
		return gh.SetStatus(statusesUrl, newSucceededBody())
	}

	// A pending status is redundant with the default status, but it allows us to
	// add more details to the status check in the UI such as a link back to the
	// check enforcer run that evaluated pending.
	return gh.SetStatus(statusesUrl, newPendingBody())
}

func handleCheckSuite(gh *GithubClient, cs *CheckSuiteWebhook) error {
	fmt.Println("Handling check suite event.")

	if cs.CheckSuite.HeadBranch == "main" {
		fmt.Println("Skipping check suite for main branch.")
		return nil
	}

	if len(gh.AppTargets) > 1 {
		checkSuites, err := gh.GetCheckSuiteStatuses(cs.GetCheckSuiteUrl())
		handleError(err)
		return handleCheckSuiteConclusions(gh, checkSuites, cs.GetStatusesUrl())
	} else {
		checkSuites := gh.FilterCheckSuiteStatuses([]CheckSuite{cs.CheckSuite})
		return handleCheckSuiteConclusions(gh, checkSuites, cs.GetStatusesUrl())
	}
}

func handleWorkflowRun(gh *GithubClient, workflowRun *WorkflowRunWebhook) error {
	fmt.Println("Handling workflow run event.")
	fmt.Println(fmt.Sprintf("Workflow run url: %s", workflowRun.HtmlUrl))
	fmt.Println(fmt.Sprintf("Workflow run commit: %s", workflowRun.HeadSha))

	if workflowRun.Event != "pull_request" || workflowRun.PullRequests == nil || len(workflowRun.PullRequests) == 0 {
		fmt.Println("Check enforcer only handles workflow_run events for pull requests. Skipping")
		return gh.SetStatus(workflowRun.GetStatusesUrl(), newPendingBody())
	}

	checkSuites, err := gh.GetCheckSuiteStatuses(workflowRun.GetCheckSuiteUrl())
	handleError(err)

	return handleCheckSuiteConclusions(gh, checkSuites, workflowRun.GetStatusesUrl())
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
