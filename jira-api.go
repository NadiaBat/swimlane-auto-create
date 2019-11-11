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

type session struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type AuthData struct {
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
	Name   string
	Action string
	Query  string
}

func updateDashboardIfNeed(oldIssue issue, newIssue issue, dashboardID int) error {
	updates, err := getSwimlaneUpdates(dashboardID, newIssue, oldIssue)
	if err != nil {
		return err
	}

	if updates.Action == removeAction {
		// remove swimlane from board
	}

	if updates.Action == createAction {
		// create swimlane on board
	}

	return nil
}

func getSwimlaneUpdates(dashboardID int, newIssue issue, oldIssue issue) (swimlaneUpdates, error) {
	newLabels := getLabelsField(newIssue.Fields)
	oldLabels := getLabelsField(oldIssue.Fields)

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

	// check, it can be need a swimlane id (not shure)
	if isNeedToRemoveSwimlane(newLabels, oldLabels) {
		result = swimlaneUpdates{
			Name:   swimlaneName,
			Action: removeAction,
		}
	}

	return result, nil
}

// we can create swimlanes not from curent sprint, don`t use now
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
		return nil, errors.Wrapf(err, "Jira api login for request failed %s", uri)
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

// POST request to login to JIRA API
func login() (AuthData, error) {
	loginData := map[string]string{"username": username, "password": password}

	request, err := json.Marshal(loginData)
	if err != nil {
		return AuthData{}, errors.Wrap(err, "Auth failed")
	}

	response, err := http.Post(loginURI, "application/json", bytes.NewBuffer(request))
	if err != nil {
		return AuthData{}, errors.Wrap(err, "Auth failed")
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return AuthData{}, errors.Wrap(err, "Auth failed")
	}

	authData := AuthData{}
	err = json.Unmarshal(body, &authData)
	if err != nil {
		return AuthData{}, errors.Wrap(err, "Auth failed")
	}

	return authData, nil
}
