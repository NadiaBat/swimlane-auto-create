package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

const labelsFieldID string = "labels"

const summaryFieldID string = "summary"

const swimlaneStoryLabel string = "swimline-story"

const createAction string = "create"

const removeAction string = "remove"

const recyclingTeamLabel string = "recycling-nsk"

const recyclingTeamDashboardID int = 351

type session struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type authData struct {
	Session session `json:"session"`
}

type swimlane struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Query       string `json:"query"`
}

type currentViewConfig struct {
	Swimlanes []swimlane `json:"swimlanes"`
}

type dashboard struct {
	CurrentViewConfig currentViewConfig `json:"currentViewConfig"`
}

type field struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type issue struct {
	Key    string  `json:"key"`
	Fields []field `json:"fields"`
}

type swimlaneUpdates struct {
	ID     int
	Name   string
	Action string
	Query  string
}

type changelog struct {
	Dataset changelogItems `json:"changelog"`
}

type changelogItems struct {
	Items []changelogItem `json:"items"`
}

type changelogItem struct {
	ToString   string `json:"toString"`
	FromString string `json:"fromString"`
	Field      string `json:"field"`
}

func updateDashboardIfNeed(newIssue issue, changelog changelog) error {
	oldLabels, newLabels := getLabelsFromChangelog(changelog.Dataset.Items)
	dashboardID := getDashboardID(newLabels)

	updates, err := getSwimlaneUpdates(dashboardID, newIssue, oldLabels, newLabels)

	if err != nil {
		return err
	}

	if updates.Action == removeAction {
		return removeSwimlane(dashboardID, updates.ID)
	}

	if updates.Action == createAction {
		createSwimlane(dashboardID, updates)
	}

	return nil
}

// @todo Check adding and removing team labels (make swimlane updates on it)
// @todo Add media team if need
func getDashboardID(newLabels []string) int {
	for _, label := range newLabels {
		if label == recyclingTeamLabel {
			return recyclingTeamDashboardID
		}
	}

	return 0
}

func removeSwimlane(dashboardID int, swimlaneID int) error {
	uri := fmt.Sprintf("%s/rest/greenhopper/1.0/swimlanes/%d/%d", apiBaseURI, dashboardID, swimlaneID)

	return deleteFromJiraAPI(uri)
}

func createSwimlane(dashboardID int, updates swimlaneUpdates) error {
	uri := fmt.Sprintf("%s/rest/greenhopper/1.0/swimlanes/%d/", apiBaseURI, dashboardID)

	data := map[string]string{"name": updates.Name, "query": updates.Query}

	body, err := json.Marshal(data)
	if err != nil {
		return errors.Wrap(err, "Can not create swimlane")
	}

	_, err = postToJiraAPI(uri, []byte(body))

	return err
}

func getSwimlaneUpdates(dashboardID int, newIssue issue, oldLabels []string, newLabels []string) (swimlaneUpdates, error) {
	currentSwimlanes, err := getCurrentSwimlanes(dashboardID)
	if err != nil {
		return swimlaneUpdates{}, err
	}

	swimlaneName := getSwimlaneName(newIssue)
	needToCreateSwimlanes := isNeedToCreateSwimlane(newLabels, oldLabels)
	if needToCreateSwimlanes && dashboardSwimlaneAlreadyExists(currentSwimlanes, swimlaneName) {
		return swimlaneUpdates{}, nil
	}

	result := swimlaneUpdates{}

	if needToCreateSwimlanes {
		result = swimlaneUpdates{
			Name:   swimlaneName,
			Action: createAction,
			Query:  getSwimlaneQuery(newIssue.Key),
		}
	}

	if isNeedToRemoveSwimlane(newLabels, oldLabels) {
		result = swimlaneUpdates{
			ID:     getSwimlaneID(swimlaneName, currentSwimlanes),
			Name:   swimlaneName,
			Action: removeAction,
		}
	}

	return result, nil
}

func getLabelsFromChangelog(items []changelogItem) ([]string, []string) {
	for _, item := range items {
		if item.Field == labelsFieldID {
			return strings.Split(item.FromString, ","), strings.Split(item.ToString, ",")
		}
	}

	return nil, nil
}

func getSwimlaneID(name string, swimlanes []swimlane) int {
	for _, swimlane := range swimlanes {
		if swimlane.Name == name {
			return swimlane.ID
		}
	}

	return 0
}

func getSprintLabel(swimlanes []swimlane) string {
	sprintSwimlaneFilter := "labels([[:space:]])*=([[:space:]])*(.+-sprint-[0-9]+)"

	for _, swimlane := range swimlanes {
		isSprintSwimlane, _ := regexp.MatchString(sprintSwimlaneFilter, swimlane.Query)
		if !isSprintSwimlane {
			continue
		}
		r, _ := regexp.Compile("labels([[:space:]])*=([[:space:]])*(.+-sprint-[0-9]+)")
		sprintSwimlaneText := r.FindString(swimlane.Query)
		sprintSwimlaneParts := strings.Split(sprintSwimlaneText, "= ")
		return sprintSwimlaneParts[1]
	}

	return ""
}

func isNeedToCreateSwimlane(newLabels []string, oldLabels []string) bool {
	return has(newLabels, swimlaneStoryLabel) && !has(oldLabels, swimlaneStoryLabel)
}

func isNeedToRemoveSwimlane(newLabels []string, oldLabels []string) bool {
	return !has(newLabels, swimlaneStoryLabel) && has(oldLabels, swimlaneStoryLabel)
}

func dashboardSwimlaneAlreadyExists(currentSwimlanes []swimlane, newSwimlane string) bool {
	for _, swimlane := range currentSwimlanes {
		if swimlane.Name == newSwimlane {
			return true
		}
	}

	return false
}

func getSwimlaneName(issue issue) string {
	for _, field := range issue.Fields {
		if field.ID == summaryFieldID {
			return fmt.Sprintf("<%s> %s", issue.Key, field.Text)
		}
	}

	return fmt.Sprintf("<%s> No summary", issue.Key)
}

func getSwimlaneQuery(issueKey string) string {
	return fmt.Sprintf("issue in linkedIssues(%s)", issueKey)
}

func getLabelsField(fields []field) []string {
	labelsText := ""

	for _, field := range fields {
		if field.ID == labelsFieldID {
			labelsText = field.Text
			break
		}
	}

	return strings.Split(labelsText, ", ")
}

func has(array []string, value string) bool {
	for _, item := range array {
		if item == value {
			return true
		}
	}

	return false
}

// GET current issue state
func getCurrentIssue(key string) (issue, error) {
	uri := fmt.Sprintf(
		"%s/rest/greenhopper/1.0/xboard/issue/details.json?rapidViewId=368&issueIdOrKey=%s",
		apiBaseURI,
		key,
	)

	body, err := getFromJiraAPI(uri)
	if err != nil {
		return issue{}, errors.Wrapf(err, "Can not get current issue %s", key)
	}

	issue := issue{}
	json.Unmarshal(body, &issue)

	return issue, nil
}

// GET dashboard current swimlanes
func getCurrentSwimlanes(dashboardID int) ([]swimlane, error) {
	dashboard, err := getCurrentDashboard(dashboardID)
	if err != nil {
		return nil, errors.Wrapf(err, "Can not get current swimlanes")
	}

	return dashboard.CurrentViewConfig.Swimlanes, nil
}

// GET dashboard current settings
func getCurrentDashboard(ID int) (dashboard, error) {
	uri := fmt.Sprintf(
		"%s/rest/greenhopper/1.0/xboard/config.json?returnDefaultBoard=false&rapidViewId=%d",
		apiBaseURI,
		ID,
	)

	body, err := getFromJiraAPI(uri)
	if err != nil {
		return dashboard{}, errors.Wrapf(err, "Can not get current dashboard %d", ID)
	}

	dashboard := dashboard{}
	json.Unmarshal(body, &dashboard)

	return dashboard, nil
}

// GET request to JIRA API
func getFromJiraAPI(uri string) ([]byte, error) {
	authData, err := login()
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "Jira api request building failed %s", uri)
	}

	request.Header.Add(
		"Cookie",
		fmt.Sprintf("%s=%s", authData.Session.Name, authData.Session.Value),
	)
	request.Header.Add("X-Atlassian-Token", "no-check")

	client := &http.Client{}
	response, err := client.Do(request)

	if err != nil {
		return nil, errors.Wrapf(err, "Jira api request failed %s", uri)
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return nil, errors.Wrapf(err, "Jira api request failed %s", uri)
	}

	return body, nil
}

func postToJiraAPI(uri string, data []byte) ([]byte, error) {
	authData, err := login()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(data)
	if err != nil {
		return nil, errors.Wrap(err, "Post request failed")
	}

	request, err := http.NewRequest("POST", uri, bytes.NewBuffer(body))
	if err != nil {
		return nil, errors.Wrapf(err, "Jira api request building failed %s", uri)
	}

	request.Header.Add(
		"Cookie",
		fmt.Sprintf("%s=%s", authData.Session.Name, authData.Session.Value),
	)
	request.Header.Add("X-Atlassian-Token", "no-check")

	client := &http.Client{}
	response, err := client.Do(request)

	if err != nil {
		return nil, errors.Wrapf(err, "Jira api request failed %s", uri)
	}

	defer response.Body.Close()
	result, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return nil, errors.Wrapf(err, "Jira api request failed %s", uri)
	}

	return result, nil
}

func deleteFromJiraAPI(uri string) error {
	authData, err := login()
	if err != nil {
		return err
	}

	request, err := http.NewRequest("DELETE", uri, nil)
	if err != nil {
		return errors.Wrapf(err, "Jira api request building failed %s", uri)
	}

	request.Header.Add(
		"Cookie",
		fmt.Sprintf("%s=%s", authData.Session.Name, authData.Session.Value),
	)
	request.Header.Add("X-Atlassian-Token", "no-check")

	client := &http.Client{}
	response, err := client.Do(request)

	if err != nil {
		return errors.Wrapf(err, "Jira api request failed %s", uri)
	}

	defer response.Body.Close()

	return nil
}

// POST request to login to JIRA API
func login() (authData, error) {
	loginData := map[string]string{"username": username, "password": password}

	request, err := json.Marshal(loginData)
	if err != nil {
		return authData{}, errors.Wrap(err, "Auth failed")
	}

	response, err := http.Post(loginURI, "application/json", bytes.NewBuffer(request))
	if err != nil {
		return authData{}, errors.Wrap(err, "Auth failed")
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return authData{}, errors.Wrap(err, "Auth failed")
	}

	result := authData{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		return authData{}, errors.Wrap(err, "Auth failed")
	}

	return result, nil
}
