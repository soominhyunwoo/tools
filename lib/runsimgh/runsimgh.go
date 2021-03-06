package runsimgh

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	ghapp "github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v27/github"
	"github.com/soominhyunwoo/tools/lib/runsimaws"
)

const primaryKey = "IntegrationType"
const tableName = "SimulationState"

type Integration struct {
	Client          *github.Client
	PR              *github.PullRequest
	ActiveCheckRun  *github.CheckRun
	State           *runsimaws.DdbTable
	IntegrationType *string
	CheckRunName    *string
	InstallationID  *string
	IntegrationID   *string
	RepoOwner       *string
	RepoName        *string
	PrNum           *string
}

// Retrieve simulation state data from DynamoDB
// Use the state data to configure the github api client and assign value to the integration fields
func (gh *Integration) ConfigFromState(awsRegion, ghAccessTokenID string) (err error) {
	gh.State = new(runsimaws.DdbTable)
	gh.State.Config(awsRegion, primaryKey, tableName)

	if err = gh.State.GetState("GitHub", gh); err != nil {
		return
	}

	if err = gh.ValidateState(); err != nil {
		return
	}

	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)
	privateKey, err := ssm.GetParameter(ghAccessTokenID)
	if err != nil {
		return
	}

	// authenticate the gh app
	transport, err := ghapp.New(http.DefaultTransport, gh.GetAppIntID(), gh.GetAppInstID(), []byte(privateKey))
	if err != nil {
		return
	}

	gh.Client = github.NewClient(&http.Client{Transport: transport})

	gh.PR, _, err = gh.Client.PullRequests.Get(context.Background(), gh.GetOwner(), gh.GetRepo(), gh.GetPrNum())
	return
}

// Config the github client and assign values to the integration fields
func (gh *Integration) ConfigFromScratch(awsRegion, privateKeyID, repoOwner, repoName, checkRunName,
	installationID, integrationID, prNum string) (err error) {
	gh.RepoOwner = &repoOwner
	gh.RepoName = &repoName
	gh.CheckRunName = &checkRunName
	gh.InstallationID = &installationID
	gh.IntegrationID = &integrationID
	gh.PrNum = &prNum
	gh.IntegrationType = aws.String("GitHub")
	gh.State = new(runsimaws.DdbTable)
	gh.State.Config(awsRegion, primaryKey, tableName)

	if err = gh.State.PutState(gh); err != nil {
		return
	}

	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)
	privateKey, err := ssm.GetParameter(privateKeyID)
	if err != nil {
		return
	}
	// authenticate the gh app
	transport, err := ghapp.New(http.DefaultTransport, gh.GetAppIntID(), gh.GetAppInstID(), []byte(privateKey))
	if err != nil {
		return
	}

	gh.Client = github.NewClient(&http.Client{Transport: transport})

	gh.PR, _, err = gh.Client.PullRequests.Get(context.Background(), gh.GetOwner(), gh.GetRepo(), gh.GetPrNum())
	return
}

func (gh *Integration) CreateNewCheckRun() (err error) {
	opt := github.CreateCheckRunOptions{
		Name:       gh.GetCheckRunName(),
		HeadBranch: gh.PR.Head.GetRef(),
		HeadSHA:    gh.PR.Head.GetSHA(),
	}

	gh.ActiveCheckRun, _, err = gh.Client.Checks.CreateCheckRun(context.Background(), gh.GetOwner(), gh.GetRepo(), opt)
	if err != nil {
		return
	}

	gh.CheckRunName = gh.ActiveCheckRun.Name

	return
}

// Search for any active check runs associated with the pull request.
// An active check run is defined as not having the "Conclusion" field set.
func (gh *Integration) SetActiveCheckRun() (err error) {
	listCheckRunResult, _, err := gh.Client.Checks.ListCheckRunsForRef(context.Background(),
		gh.GetOwner(), gh.GetRepo(), gh.PR.Head.GetRef(),
		&github.ListCheckRunsOptions{
			CheckName: aws.String(gh.GetCheckRunName()),
			Filter:    aws.String("latest"),
		})
	if err != nil {
		return
	}
	if len(listCheckRunResult.CheckRuns) == 0 || listCheckRunResult.CheckRuns[0].GetConclusion() != "" {
		return errors.New("ErrorNoActiveCheckRunsFound")
	}
	gh.ActiveCheckRun = listCheckRunResult.CheckRuns[0]
	return
}

// Retrieve a new copy of the active check run. New copy contains any fields which have been updated since
// the active check run has been set
func (gh *Integration) UpdateActiveCheckRun() (err error) {
	gh.ActiveCheckRun, _, err = gh.Client.Checks.GetCheckRun(context.Background(),
		gh.GetOwner(), gh.GetRepo(), gh.ActiveCheckRun.GetID())
	if err != nil {
		return
	}
	return
}

func (gh *Integration) ConcludeCheckRun(summary, conclusion *string) (err error) {
	opt := github.UpdateCheckRunOptions{
		Name:        gh.ActiveCheckRun.GetName(),
		Status:      aws.String("completed"),
		CompletedAt: &github.Timestamp{Time: time.Now()},
		Conclusion:  conclusion,

		Output: &github.CheckRunOutput{
			Title:   aws.String("Details"),
			Summary: summary,
		},
	}

	gh.ActiveCheckRun, _, err = gh.Client.Checks.UpdateCheckRun(context.Background(), gh.GetOwner(), gh.GetRepo(),
		gh.ActiveCheckRun.GetID(), opt)

	return
}

func (gh *Integration) UpdateCheckRunStatus(status, summary *string) (err error) {
	opt := github.UpdateCheckRunOptions{
		Name:       gh.ActiveCheckRun.GetName(),
		HeadBranch: gh.PR.Head.Ref,
		HeadSHA:    gh.PR.Head.SHA,
		Status:     status,
	}
	if summary != nil {
		opt.Output = &github.CheckRunOutput{
			Title:   aws.String("Details"),
			Summary: summary,
		}
	}

	gh.ActiveCheckRun, _, err = gh.Client.Checks.UpdateCheckRun(context.Background(), gh.GetOwner(), gh.GetRepo(),
		gh.ActiveCheckRun.GetID(), opt)

	return
}

func (gh *Integration) DeleteState() (err error) {
	return gh.State.DeleteState("GitHub")
}

func (gh *Integration) GetOwner() string {
	return *gh.RepoOwner
}

func (gh *Integration) GetRepo() string {
	return *gh.RepoName
}

func (gh *Integration) GetCheckRunName() string {
	return *gh.CheckRunName
}

func (gh *Integration) GetPrNum() (num int) {
	num, err := strconv.Atoi(*gh.PrNum)
	if err != nil {
		panic(err)
	}
	return
}

func (gh *Integration) GetAppInstID() (id int) {
	id, err := strconv.Atoi(*gh.InstallationID)
	if err != nil {
		panic(err)
	}
	return
}

func (gh *Integration) GetAppIntID() (id int) {
	id, err := strconv.Atoi(*gh.IntegrationID)
	if err != nil {
		panic(err)
	}
	return
}

func (gh *Integration) ValidateState() (err error) {
	if gh.IntegrationID == nil {
		return errors.New("ErrorMissingAttribute: IntegrationID")
	}
	if gh.InstallationID == nil {
		return errors.New("ErrorMissingAttribute: InstallationID ")
	}
	if gh.PrNum == nil {
		return errors.New("ErrorMissingAttribute: PrNum")
	}
	if gh.RepoName == nil {
		return errors.New("ErrorMissingAttribute: RepoName")
	}
	if gh.RepoOwner == nil {
		return errors.New("ErrorMissingAttribute: RepoOwner")
	}
	if gh.CheckRunName == nil {
		return errors.New("ErrorMissingAttribute: CheckRunName")
	}
	return
}
