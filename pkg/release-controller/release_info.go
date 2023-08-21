package releasecontroller

import (
	"bytes"
	"context"
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	jiraBaseClient "github.com/andygrunwald/go-jira"
	"github.com/golang/groupcache"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog"
	"k8s.io/test-infra/prow/jira"
)

const (
	sourceJira                       = "jira"
	JiraCustomFieldEpicLink          = "customfield_12311140"
	JiraCustomFieldFeatureLinkOnEpic = "customfield_12313140"
	JiraCustomFieldFeatureLink       = "customfield_12318341"
	JiraCustomFieldReleaseNotes      = "customfield_12310211"
	JiraTypeSubTask                  = "Sub-task"
	JiraTypeEpic                     = "Epic"
	JiraTypeFeature                  = "Feature"
	JiraTypeStory                    = "Story"
	JiraTypeMarketProblem            = "Market Problem"
)

const maxChunkSize = 500

type CachingReleaseInfo struct {
	cache *groupcache.Group
}

func NewCachingReleaseInfo(info ReleaseInfo, size int64, architecture string) ReleaseInfo {
	cache := groupcache.NewGroup("release", size, groupcache.GetterFunc(func(ctx context.Context, key string, sink groupcache.Sink) error {
		var s string
		var err error
		parts := strings.Split(key, "\x00")
		switch parts[0] {
		case "bugs":
			if strings.Contains(parts[1], "\x00") || strings.Contains(parts[2], "\x00") {
				s, err = "", fmt.Errorf("invalid from/to")
			} else {
				var bugDetailsArr []BugDetails
				bugDetailsArr, err = info.Bugs(parts[1], parts[2])
				if err == nil {
					var bugDetailsByte []byte
					bugDetailsByte, err = json.Marshal(bugDetailsArr)
					if err != nil {
						klog.V(4).Infof("Failed to Marshal Bug Details Array; from: %s to: %s; %s", parts[1], parts[2], err)
					} else {
						s = string(bugDetailsByte)
					}
				}
			}
		case "changelog":
			if strings.Contains(parts[1], "\x00") || strings.Contains(parts[2], "\x00") || strings.Contains(parts[3], "\x00") {
				s, err = "", fmt.Errorf("invalid from/to")
			} else {
				isJson, parseErr := strconv.ParseBool(parts[3])
				if parseErr != nil {
					s, err = "", fmt.Errorf("unable to parse boolean value")
				} else {
					s, err = info.ChangeLog(parts[1], parts[2], isJson)
				}
			}
		case "releaseinfo":
			s, err = info.ReleaseInfo(parts[1])
		case "imageinfo":
			s, err = info.ImageInfo(parts[1], architecture)
		case "issuesinfo":
			s, err = info.IssuesInfo(parts[1])
		case "featurechildren":
			s, err = info.GetFeatureChildren(strings.Split(parts[1], ";"), 10*time.Minute)
		}
		if err != nil {
			return err
		}
		sink.SetString(s)
		return nil
	}))
	return &CachingReleaseInfo{
		cache: cache,
	}
}

func (c *CachingReleaseInfo) Bugs(from, to string) ([]BugDetails, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"bugs", from, to}, "\x00"), groupcache.StringSink(&s))
	if err != nil {
		return []BugDetails{}, err
	}
	return bugList(s)
}

func (c *CachingReleaseInfo) ChangeLog(from, to string, json bool) (string, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"changelog", from, to, strconv.FormatBool(json)}, "\x00"), groupcache.StringSink(&s))
	return s, err
}

func (c *CachingReleaseInfo) ReleaseInfo(image string) (string, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"releaseinfo", image}, "\x00"), groupcache.StringSink(&s))
	return s, err
}

func (c *CachingReleaseInfo) UpgradeInfo(image string) (ReleaseUpgradeInfo, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"releaseinfo", image}, "\x00"), groupcache.StringSink(&s))
	if err != nil {
		return ReleaseUpgradeInfo{}, err
	}
	return releaseInfoToUpgradeInfo(s)
}

func (c *CachingReleaseInfo) IssuesInfo(changelog string) (string, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"issuesinfo", changelog}, "\x00"), groupcache.StringSink(&s))
	return s, err
}

func (c *CachingReleaseInfo) ImageInfo(image, archtecture string) (string, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"imageinfo", image}, "\x00"), groupcache.StringSink(&s))
	return s, err
}

func (c *CachingReleaseInfo) GetFeatureChildren(featuresList []string, validityPeriod time.Duration) (string, error) {
	var s string
	roundedTime := time.Now().Round(validityPeriod)
	sort.Strings(featuresList)
	key := strings.Join([]string{"featurechildren", strings.Join(featuresList, ";")}, "\x00")
	keyWithTime := key + "_" + roundedTime.Format(time.RFC3339)
	err := c.cache.Get(context.TODO(), keyWithTime, groupcache.StringSink(&s))
	return s, err
}

type ReleaseInfo interface {
	// Bugs returns a list of jira bug IDs for bugs fixed between the provided release tags
	Bugs(from, to string) ([]BugDetails, error)
	ChangeLog(from, to string, json bool) (string, error)
	ReleaseInfo(image string) (string, error)
	UpgradeInfo(image string) (ReleaseUpgradeInfo, error)
	ImageInfo(image, architecture string) (string, error)
	IssuesInfo(changelog string) (string, error)
	GetFeatureChildren(featuresList []string, validityPeriod time.Duration) (string, error)
}

type ExecReleaseInfo struct {
	client      kubernetes.Interface
	restConfig  *rest.Config
	namespace   string
	name        string
	imageNameFn func() (string, error)
	jiraClient  jira.Client
}

// NewExecReleaseInfo creates a stateful set, in the specified namespace, that provides git changelogs to the
// Release Status website.  The provided name will prevent other instances of the stateful set
// from being created when created with an identical name.
func NewExecReleaseInfo(client kubernetes.Interface, restConfig *rest.Config, namespace string, name string, imageNameFn func() (string, error), jiraClient jira.Client) *ExecReleaseInfo {
	return &ExecReleaseInfo{
		client:      client,
		restConfig:  restConfig,
		namespace:   namespace,
		name:        name,
		imageNameFn: imageNameFn,
		jiraClient:  jiraClient,
	}
}

func (r *ExecReleaseInfo) ReleaseInfo(image string) (string, error) {
	if _, err := imagereference.Parse(image); err != nil {
		return "", fmt.Errorf("%s is not an image reference: %v", image, err)
	}
	cmd := []string{"oc", "adm", "release", "info", "-o", "json", image}

	u := r.client.CoreV1().RESTClient().Post().Resource("pods").Namespace(r.namespace).Name("git-cache-0").SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: "git",
		Stdout:    true,
		Stderr:    true,
		Command:   cmd,
	}, scheme.ParameterCodec).URL()

	e, err := remotecommand.NewSPDYExecutor(r.restConfig, "POST", u)
	if err != nil {
		return "", fmt.Errorf("could not initialize a new SPDY executor: %v", err)
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if err := e.Stream(remotecommand.StreamOptions{
		Stdout: out,
		Stdin:  nil,
		Stderr: errOut,
	}); err != nil {
		klog.V(4).Infof("Failed to get release info for %s: %v\n$ %s\n%s\n%s", image, err, strings.Join(cmd, " "), errOut.String(), out.String())
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return "", fmt.Errorf("could not get release info for %s: %v", image, msg)
	}
	return out.String(), nil
}

func (r *ExecReleaseInfo) ChangeLog(from, to string, isJson bool) (string, error) {
	if _, err := imagereference.Parse(from); err != nil {
		return "", fmt.Errorf("%s is not an image reference: %v", from, err)
	}
	if _, err := imagereference.Parse(to); err != nil {
		return "", fmt.Errorf("%s is not an image reference: %v", to, err)
	}
	if strings.HasPrefix(from, "-") || strings.HasPrefix(to, "-") {
		return "", fmt.Errorf("not a valid reference")
	}

	cmd := []string{"oc", "adm", "release", "info", "--changelog=/tmp/git/", from, to}
	if isJson {
		cmd = append(cmd, "--output=json")
	}
	klog.V(4).Infof("Running changelog command: %s", strings.Join(cmd, " "))
	u := r.client.CoreV1().RESTClient().Post().Resource("pods").Namespace(r.namespace).Name("git-cache-0").SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: "git",
		Stdout:    true,
		Stderr:    true,
		Command:   cmd,
	}, scheme.ParameterCodec).URL()

	e, err := remotecommand.NewSPDYExecutor(r.restConfig, "POST", u)
	if err != nil {
		return "", fmt.Errorf("could not initialize a new SPDY executor: %v", err)
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if err := e.Stream(remotecommand.StreamOptions{
		Stdout: out,
		Stdin:  nil,
		Stderr: errOut,
	}); err != nil {
		klog.V(4).Infof("Failed to generate changelog: %v\n$ %s\n%s\n%s", err, strings.Join(cmd, " "), errOut.String(), out.String())
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return "", fmt.Errorf("could not generate a changelog: %v", msg)
	}
	if isJson {
		var changeLog ChangeLog
		if err := json.Unmarshal(out.Bytes(), &changeLog); err != nil {
			klog.Warningf("invalid changelog JSON: %v", err)
			return "", err
		}
	}
	return out.String(), nil
}

func (r *ExecReleaseInfo) Bugs(from, to string) ([]BugDetails, error) {
	if _, err := imagereference.Parse(from); err != nil {
		return nil, fmt.Errorf("%s is not an image reference: %v", from, err)
	}
	if _, err := imagereference.Parse(to); err != nil {
		return nil, fmt.Errorf("%s is not an image reference: %v", to, err)
	}
	if strings.HasPrefix(from, "-") || strings.HasPrefix(to, "-") {
		return nil, fmt.Errorf("not a valid reference")
	}

	cmd := []string{"oc", "adm", "release", "info", "--bugs=/tmp/git/", "--output=json", "--skip-bug-check", from, to}
	klog.V(4).Infof("Running bugs command: %s", strings.Join(cmd, " "))
	u := r.client.CoreV1().RESTClient().Post().Resource("pods").Namespace(r.namespace).Name("git-cache-0").SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: "git",
		Stdout:    true,
		Stderr:    true,
		Command:   cmd,
	}, scheme.ParameterCodec).URL()

	e, err := remotecommand.NewSPDYExecutor(r.restConfig, "POST", u)
	if err != nil {
		return nil, fmt.Errorf("could not initialize a new SPDY executor: %v", err)
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if err := e.Stream(remotecommand.StreamOptions{
		Stdout: out,
		Stdin:  nil,
		Stderr: errOut,
	}); err != nil {
		klog.V(4).Infof("Failed to generate bug list: %v\n$ %s\n%s\n%s", err, strings.Join(cmd, " "), errOut.String(), out.String())
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return []BugDetails{}, fmt.Errorf("could not generate a bug list: %v", msg)
	}

	return bugList(out.String())
}

func bugList(s string) ([]BugDetails, error) {
	if s == "" {
		return []BugDetails{}, nil
	}
	var bugList []BugDetails
	err := json.Unmarshal([]byte(s), &bugList)
	if err != nil {
		return []BugDetails{}, err
	}
	return bugList, nil
}

type BugDetails struct {
	ID     string `json:"id"`
	Source int    `json:"source"`
}

type ReleaseUpgradeInfo struct {
	Metadata *ReleaseUpgradeMetadata `json:"metadata"`
}

type ReleaseUpgradeMetadata struct {
	Version  string   `json:"version"`
	Previous []string `json:"previous"`
}

func releaseInfoToUpgradeInfo(s string) (ReleaseUpgradeInfo, error) {
	var tagUpgradeInfo ReleaseUpgradeInfo
	if err := json.Unmarshal([]byte(s), &tagUpgradeInfo); err != nil {
		return ReleaseUpgradeInfo{}, fmt.Errorf("could not unmarshal release info for tag %s: %v", s, err)
	}
	return tagUpgradeInfo, nil
}

func (r *ExecReleaseInfo) UpgradeInfo(image string) (ReleaseUpgradeInfo, error) {
	out, err := r.ReleaseInfo(image)
	if err != nil {
		return ReleaseUpgradeInfo{}, fmt.Errorf("could not get upgrade info for %s: %v", image, err)
	}
	return releaseInfoToUpgradeInfo(out)
}

func (r *ExecReleaseInfo) ImageInfo(image, architecture string) (string, error) {
	if _, err := imagereference.Parse(image); err != nil {
		return "", fmt.Errorf("%s is not an image reference: %v", image, err)
	}
	if len(architecture) == 0 || architecture == "multi" {
		architecture = "amd64"
	}
	cmd := []string{"oc", "image", "info", "--filter-by-os", fmt.Sprintf("linux/%s", architecture), "-o", "json", image}

	u := r.client.CoreV1().RESTClient().Post().Resource("pods").Namespace(r.namespace).Name("git-cache-0").SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: "git",
		Stdout:    true,
		Stderr:    true,
		Command:   cmd,
	}, scheme.ParameterCodec).URL()

	e, err := remotecommand.NewSPDYExecutor(r.restConfig, "POST", u)
	if err != nil {
		return "", fmt.Errorf("could not initialize a new SPDY executor: %v", err)
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if err := e.Stream(remotecommand.StreamOptions{
		Stdout: out,
		Stdin:  nil,
		Stderr: errOut,
	}); err != nil {
		klog.V(4).Infof("Failed to get image info for %s: %v\n$ %s\n%s\n%s", image, err, strings.Join(cmd, " "), errOut.String(), out.String())
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return "", fmt.Errorf("could not get image info for %s: %v", image, msg)
	}
	return out.String(), nil
}

func (r *ExecReleaseInfo) IssuesInfo(changelog string) (string, error) {
	var c ChangeLog
	err := json.Unmarshal([]byte(changelog), &c)
	if err != nil {
		return "", err
	}
	issuesToPRMap := extractIssuesFromChangeLog(c, sourceJira)
	issuesList := func() []string {
		result := make([]string, 0)
		for key := range issuesToPRMap {
			result = append(result, key)
		}
		return result
	}()
	issues, err := r.GetIssuesWithChunks(issuesList)

	if err != nil {
		return "", err
	}

	// fetch the parent issues recursively
	currentIssues := issues
	visited := make(map[string]bool)
	err = r.JiraRecursiveGet(currentIssues, &issues, visited, 10000)
	if err != nil {
		return "", err
	}
	t := TransformJiraIssues(issues, issuesToPRMap)
	s, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return string(s), nil
}

func (r *ExecReleaseInfo) JiraRecursiveGet(issues []jiraBaseClient.Issue, allIssues *[]jiraBaseClient.Issue, visited map[string]bool, limit int) error {
	// add a fail-safe to protect against stack-overflows, in case of an infinite cycle
	if limit == 0 {
		return stdErrors.New("maximum recursion depth reached")
	}
	var parents []string
	for _, issue := range issues {
		// skip processing already processed issues. This will protect against cyclic loops and redundant work
		if visited[issue.Key] {
			continue
		}

		visited[issue.Key] = true
		if issue.Fields.Type.Name == JiraTypeSubTask {
			if issue.Fields.Parent != nil {
				// TODO - check if this is expected - we do not check for parents of Features, since we consider that to be the root
				if issue.Fields.Parent.Key != "" && issue.Fields.Type.Name != JiraTypeFeature {
					parents = append(parents, issue.Fields.Parent.Key)
					continue
				}
			}
		}
		// TODO - check if this is expected - we do not check for parents of Features, since we consider that to be the root
		if issue.Fields.Type.Name != JiraTypeFeature {
			for key, f := range issue.Fields.Unknowns {
				if f != nil {
					switch f.(type) {
					case string:
						if key == JiraCustomFieldEpicLink || key == JiraCustomFieldFeatureLinkOnEpic {
							parents = append(parents, typeCheck(f))

						}
					case map[string]interface{}:
						if key == JiraCustomFieldFeatureLink {
							parents = append(parents, typeCheck(f))
						}
					}
				}
			}
		}

	}
	if len(parents) > 0 {
		p, err := r.GetIssuesWithChunks(parents)
		if err != nil {
			// TODO - retry maybe?
			return err
		}
		*allIssues = append(*allIssues, p...)
		err = r.JiraRecursiveGet(p, allIssues, visited, limit-1)
		if err != nil {
			return err
		}
	}
	return nil
}

type IssueDetails struct {
	Summary        string
	Status         string
	Parent         string
	Feature        string
	Epic           string
	IssueType      string
	Description    string
	ReleaseNotes   string
	PRs            []string
	ResolutionDate time.Time
	Transitions    []Transition
}

func typeCheck(o interface{}) string {
	switch o.(type) {
	case string:
		return o.(string)
	case map[string]interface{}:
		t := o.(map[string]interface{})
		return typeCheck(t["key"])
	default:
		klog.Warningf("unable to parse the Jira unknown field: %v\n", o)
		return ""
	}
}

func TransformJiraIssues(issues []jiraBaseClient.Issue, prMap map[string][]string) map[string]IssueDetails {
	t := make(map[string]IssueDetails, 0)
	for _, issue := range issues {
		var epic, feature, releaseNotes, parent string
		for unknownField, value := range issue.Fields.Unknowns {
			if value != nil {
				switch unknownField {
				case JiraCustomFieldEpicLink:
					epic = typeCheck(value)
				case JiraCustomFieldFeatureLinkOnEpic:
					feature = typeCheck(value)
				case JiraCustomFieldFeatureLink:
					feature = typeCheck(value)
				case JiraCustomFieldReleaseNotes:
					releaseNotes = typeCheck(value)
				}
			}
		}
		// TODO - there must be a better way to do this
		if issue.Fields.Type.Name != JiraTypeSubTask && epic != "" {
			feature = ""
		} else {
			if issue.Fields.Type.Name == JiraTypeSubTask {
				if issue.Fields.Parent != nil {
					feature = ""
				}
			}
		}
		if issue.Fields.Parent != nil {
			if issue.Fields.Parent.Key != "" {
				parent = issue.Fields.Parent.Key
			}
		}
		t[issue.Key] = IssueDetails{
			Summary:     checkJiraSecurity(&issue, issue.Fields.Summary),
			Status:      issue.Fields.Status.Name,
			Parent:      parent,
			Feature:     feature,
			Epic:        epic,
			IssueType:   issue.Fields.Type.Name,
			Description: checkJiraSecurity(&issue, issue.RenderedFields.Description),
			// TODO- check if this should be restricted as well
			ReleaseNotes:   releaseNotes,
			PRs:            prMap[issue.Key],
			ResolutionDate: time.Time(issue.Fields.Resolutiondate),
			Transitions:    extractTransitions(issue.Changelog),
		}
	}
	return t
}

type Transition struct {
	FromStatus string
	ToStatus   string
	Time       time.Time
}

func extractTransitions(changelog *jiraBaseClient.Changelog) []Transition {
	transitions := make([]Transition, 0)
	for _, history := range changelog.Histories {
		transitionTime, err := time.Parse("2006-01-02T15:04:05.999-0700", history.Created)
		if err != nil {
			klog.Errorf("failed to parse string to time.Time: %s", err)
		}
		for _, item := range history.Items {
			if item.Field == "status" {
				transitions = append(transitions, Transition{
					FromStatus: item.FromString,
					ToStatus:   item.ToString,
					Time:       transitionTime,
				})
			}
		}
	}
	return transitions
}

func checkJiraSecurity(issue *jiraBaseClient.Issue, data string) string {
	securityField, err := jira.GetIssueSecurityLevel(issue)
	if err != nil {
		return data
	}
	// TODO - if the security field name is set (or any field for that matter), we restrict the issue. Check if a whitelist/blacklist is necessary
	if securityField.Name != "" {
		return fmt.Sprintf("The details of this Jira Card are restricted (%s)", securityField.Description)
	} else {
		return data
	}
}

func (r *ExecReleaseInfo) GetFeatureChildren(featuresList []string, validityPeriod time.Duration) (string, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	limit := make(chan struct{}, 10) // set the limit to 10 concurrent goroutines
	results := make(map[string][]jiraBaseClient.Issue)
	// This will prevent a Panic if/when the release-controller's are run without the necessary jira flags
	if r.jiraClient == nil {
		return "", fmt.Errorf("unable to communicate with Jira")
	}
	// loop to start goroutines
	for _, feature := range featuresList {
		wg.Add(1)
		limit <- struct{}{}
		go func(id string) {
			defer func() { <-limit }()
			defer wg.Done()
			a, _, err := r.jiraClient.SearchWithContext(
				context.Background(),
				fmt.Sprintf("issuekey in childIssuesOf(%s) AND issuetype in (Epic)", id),
				&jiraBaseClient.SearchOptions{
					MaxResults: 500, // TODO : here we assume that a feature will not have more than 500 epics, if so, we need to paginate
					Fields: []string{
						"key",
						"status",
						"resolutiondate",
					},
				},
			)
			if err != nil {
				return
			}
			mu.Lock()
			results[id] = a
			mu.Unlock()
		}(feature)
	}

	wg.Wait()
	close(limit)
	a, err := json.Marshal(results)
	return string(a), err
}

func (r *ExecReleaseInfo) GetIssuesWithChunks(issues []string) (result []jiraBaseClient.Issue, err error) {
	// This will prevent a Panic if/when the release-controller's are run without the necessary jira flags
	if r.jiraClient == nil {
		return result, fmt.Errorf("unable to communicate with Jira")
	}
	// Keep the chunk on the small side, it is much faster
	// There is a limit for API calls per second in Akamai for Jira, don't chunk too much
	chunk := len(issues) / 10

	// Jira can't handle more than 500 IDs at once
	if chunk > maxChunkSize {
		chunk = maxChunkSize
	}
	if chunk < 50 {
		chunk = 50
	}
	// Divide issues into chunks
	dividedIssues := divideSlice(issues, chunk)

	// Search for issues in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	var buf bytes.Buffer
	for _, parts := range dividedIssues {
		wg.Add(1)
		jql := fmt.Sprintf("id IN (%s)", strings.Join(parts, ","))
		go func(jql string) {
			defer wg.Done()
			issues, _, err := r.jiraClient.SearchWithContext(
				context.Background(),
				jql,
				&jiraBaseClient.SearchOptions{
					MaxResults: chunk + 1,
					Fields: []string{
						"security",
						"summary",
						JiraCustomFieldEpicLink,
						JiraCustomFieldFeatureLinkOnEpic,
						JiraCustomFieldReleaseNotes,
						JiraCustomFieldFeatureLink,
						"issuetype",
						"description",
						"resolutiondate",
						"status",
						"parent",
					},
					Expand: "renderedFields,changelog",
				},
			)
			if err != nil {
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					err = fmt.Errorf("search failed: %w", err)
					buf.WriteString(err.Error() + "\n")
				}
				return
			}
			mu.Lock()
			defer mu.Unlock()
			result = append(result, issues...)
		}(jql)
	}
	wg.Wait()

	// Combine any errors
	if buf.Len() > 0 {
		err = stdErrors.New(buf.String())
	}

	return result, err
}

func divideSlice(issues []string, chunk int) [][]string {
	var divided [][]string
	for index := 0; index < len(issues); index += chunk {
		end := index + chunk
		if end > len(issues) {
			end = len(issues)
		}
		divided = append(divided, issues[index:end])
	}
	return divided
}

func extractIssuesFromChangeLog(changelog ChangeLog, bugSource string) map[string][]string {
	issues := make(map[string][]string, 0)
	changeLogImageInfo := make(map[string][]ChangeLogImageInfo, 0)
	changeLogImageInfo["NewImages"] = changelog.NewImages
	changeLogImageInfo["RemovedImages"] = changelog.RemovedImages
	changeLogImageInfo["RebuiltImages"] = changelog.RebuiltImages
	changeLogImageInfo["UpdatedImages"] = changelog.UpdatedImages
	for _, k := range changeLogImageInfo {
		for _, changeLogImageInfo := range k {
			for _, commit := range changeLogImageInfo.Commits {
				switch bugSource {
				case sourceJira:
					for key := range commit.Issues {
						issues[key] = append(issues[key], commit.PullURL)
					}
				}
			}
		}
	}
	return issues

}

func (r *ExecReleaseInfo) RefreshPod() error {
	sts, err := r.client.AppsV1().StatefulSets(r.namespace).Get(context.TODO(), "git-cache", metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		sts = nil
	}

	if sts != nil && len(sts.Annotations["release-owner"]) > 0 && sts.Annotations["release-owner"] != r.name {
		klog.Infof("Another release controller is managing git-cache, ignoring")
		return nil
	}

	image, err := r.imageNameFn()
	if err != nil {
		return fmt.Errorf("unable to load image for caching git: %v", err)
	}
	spec := r.specHash(image)

	if sts == nil {
		sts = &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "git-cache",
				Namespace:   r.namespace,
				Annotations: map[string]string{"release-owner": r.name},
			},
			Spec: spec,
		}
		if _, err := r.client.AppsV1().StatefulSets(r.namespace).Create(context.TODO(), sts, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("can't create stateful set for cache: %v", err)
		}
		return nil
	}

	sts.Spec = spec
	if _, err := r.client.AppsV1().StatefulSets(r.namespace).Update(context.TODO(), sts, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("can't update stateful set for cache: %v", err)
	}
	return nil
}

func (r *ExecReleaseInfo) specHash(image string) appsv1.StatefulSetSpec {
	isTrue := true
	spec := appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app":     "git-cache",
				"release": r.name,
			},
		},
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{},
		},
		VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "git",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"storage": resource.MustParse("200Gi"),
						},
					},
				},
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app":     "git-cache",
					"release": r.name,
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "git-credentials", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "git-credentials"}}},
					{Name: "pull-secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "git-pull-secret", Optional: &isTrue}}},
				},
				InitContainers: []corev1.Container{
					{
						Name:  "git",
						Image: image,
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: "/tmp"},
							{Name: "XDG_RUNTIME_DIR", Value: "/tmp/run"},
							{Name: "GIT_COMMITTER_NAME", Value: "test"},
							{Name: "GIT_COMMITTER_EMAIL", Value: "test@test.com"},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "git", MountPath: "/tmp/git/"},
							{Name: "git-credentials", MountPath: "/tmp/.git-credentials", SubPath: ".git-credentials"},
							{Name: "pull-secret", MountPath: "/tmp/pull-secret"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
						Command: []string{
							"/bin/bash",
							"-c",
							`#!/bin/bash
							set -euo pipefail
							trap 'kill $(jobs -p); exit 0' TERM

							SECONDS=0

							# ensure we are logged in to our registry
							mkdir -p /tmp/.docker/ ${XDG_RUNTIME_DIR}
							cp /tmp/pull-secret/* /tmp/.docker/ || true

							git config --global credential.helper store
							git config --global user.name test
							git config --global user.email test@test.com
							oc registry login --to /tmp/.docker/config.json

							if [ -x /tmp/git/github.com ]
							then
							  echo "Performing git maintenance..."
							  for repo in $(find /tmp/git -type d -a -name .git | xargs dirname)
							  do
								(cd $repo && git gc && git pull)
							  done
							else
							  FROM=$(curl -s https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted | jq -r '.["4-stable"][0] // empty')
							  TO=$(curl -s https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted | jq -r '.["4-dev-preview"][0] // empty')

							  if [[ -n "$FROM" && -n "$TO" ]]
							  then
								echo "Pre-populating the git cache..."
								oc adm release info --changelog=/tmp/git quay.io/openshift-release-dev/ocp-release:$FROM-x86_64 quay.io/openshift-release-dev/ocp-release:$TO-x86_64
							  else
								echo "Unable to Pre-populate the git cache!"
							  fi
							fi

							DURATION=$SECONDS
							echo "Took: $(($DURATION / 60))m $(($DURATION % 60))s"
							`,
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "git",
						Image: image,
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: "/tmp"},
							{Name: "XDG_RUNTIME_DIR", Value: "/tmp/run"},
							{Name: "GIT_COMMITTER_NAME", Value: "test"},
							{Name: "GIT_COMMITTER_EMAIL", Value: "test@test.com"},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "git", MountPath: "/tmp/git/"},
							{Name: "git-credentials", MountPath: "/tmp/.git-credentials", SubPath: ".git-credentials"},
							{Name: "pull-secret", MountPath: "/tmp/pull-secret"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
						Command: []string{
							"/bin/bash",
							"-c",
							`#!/bin/bash
							set -euo pipefail
							trap 'kill $(jobs -p); exit 0' TERM

							# ensure we are logged in to our registry
							mkdir -p /tmp/.docker/ "${XDG_RUNTIME_DIR}"
							cp /tmp/pull-secret/* /tmp/.docker/ || true

							git config --global credential.helper store
							git config --global user.name test
							git config --global user.email test@test.com
							oc registry login --to /tmp/.docker/config.json
							while true; do
							  sleep 180 & wait
							done
							`,
						},
					},
				},
			},
		},
	}
	return spec
}

type ExecReleaseFiles struct {
	client           kubernetes.Interface
	restConfig       *rest.Config
	namespace        string
	name             string
	releaseNamespace string
	registry         string
	imageNameFn      func() (string, error)
}

// NewExecReleaseFiles creates a stateful set, in the specified namespace, that provides cached access to downloaded
// installer images from the Release Status website.  The provided name will prevent other instances of the stateful set
// from being created when created with an identical name.  The releaseNamespace is used to ensure that the tools are
// downloaded from the correct namespace.
func NewExecReleaseFiles(client kubernetes.Interface, restConfig *rest.Config, namespace string, name string, releaseNamespace string, registry string, imageNameFn func() (string, error)) *ExecReleaseFiles {
	return &ExecReleaseFiles{
		client:           client,
		restConfig:       restConfig,
		namespace:        namespace,
		name:             name,
		releaseNamespace: releaseNamespace,
		registry:         registry,
		imageNameFn:      imageNameFn,
	}
}

func (r *ExecReleaseFiles) RefreshPod() error {
	sts, err := r.client.AppsV1().StatefulSets(r.namespace).Get(context.TODO(), "files-cache", metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		sts = nil
	}

	if sts != nil && len(sts.Annotations["release-owner"]) > 0 && sts.Annotations["release-owner"] != r.name {
		klog.Infof("Another release controller is managing files-cache, ignoring")
		return nil
	}

	image, err := r.imageNameFn()
	if err != nil {
		return fmt.Errorf("unable to load image for caching git: %v", err)
	}
	spec := r.specHash(image)

	if sts == nil {
		sts = &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "files-cache",
				Namespace:   r.namespace,
				Annotations: map[string]string{"release-owner": r.name},
			},
			Spec: spec,
		}
		if _, err := r.client.AppsV1().StatefulSets(r.namespace).Create(context.TODO(), sts, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("can't create stateful set for cache: %v", err)
		}
		return nil
	}

	sts.Spec = spec
	if _, err := r.client.AppsV1().StatefulSets(r.namespace).Update(context.TODO(), sts, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("can't update stateful set for cache: %v", err)
	}
	return nil
}

func (r *ExecReleaseFiles) specHash(image string) appsv1.StatefulSetSpec {
	one := int64(1)

	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/",
				Port:   intstr.FromInt(8080),
				Scheme: corev1.URISchemeHTTP,
			},
		},
		InitialDelaySeconds: 3,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      1,
	}

	isTrue := true
	spec := appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app":     "files-cache",
				"release": r.name,
			},
		},
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{},
		},
		VolumeClaimTemplates: []corev1.PersistentVolumeClaim{},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app":     "files-cache",
					"release": r.name,
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{Name: "pull-secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "files-pull-secret", Optional: &isTrue}}},
				},
				TerminationGracePeriodSeconds: &one,
				Containers: []corev1.Container{
					{
						Name:       "files",
						Image:      image,
						WorkingDir: "/srv/cache",
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: "/tmp"},
							{Name: "XDG_RUNTIME_DIR", Value: "/tmp/run"},
							{Name: "RELEASE_NAMESPACE", Value: r.releaseNamespace},
							{Name: "REGISTRY", Value: r.registry},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "cache", MountPath: "/srv/cache/"},
							{Name: "pull-secret", MountPath: "/tmp/pull-secret"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("200Mi"),
							},
						},

						Command: []string{"/bin/bash", "-c"},
						Args: []string{
							`
#!/bin/bash

set -euo pipefail
trap 'kill $(jobs -p); exit 0' TERM

# ensure we are logged in to our registry
mkdir -p /tmp/.docker/ "${XDG_RUNTIME_DIR}"
cp /tmp/pull-secret/* /tmp/.docker/ || true
oc registry login --to /tmp/.docker/config.json

if which python3 2> /dev/null; then
  # If python3 is available, use it
  cat <<END >/tmp/serve.py
import re, os, subprocess, time, threading, socket, socketserver, http, http.server, glob
from subprocess import CalledProcessError

handler = http.server.SimpleHTTPRequestHandler

RELEASE_NAMESPACE = os.getenv('RELEASE_NAMESPACE', 'ocp')
REGISTRY = os.getenv('REGISTRY', 'registry.ci.openshift.org')

files = glob.glob('/srv/cache/**/DOWNLOADING.md', recursive=True)
for file in files:
    print(f"Removing: {file}")
    os.remove(file)


def check_stale_download(path):
    service_list = subprocess.Popen(['ps', '-ef'], stdout=subprocess.PIPE)
    filtered_list = subprocess.Popen(['grep', f'[o]c adm release extract --tools --to {path}'], stdin=service_list.stdout,
                                     stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    service_list.stdout.close()
    out, err = filtered_list.communicate()
    if out.decode("utf-8") != '' and err.decode("utf-8") == '':
        return False
    return True


class FileServer(handler):
    def _present_default_content(self, name):
        content = ("""<!DOCTYPE html>
        <html>
            <head>
                <meta http-equiv=\"refresh\" content=\"5\">
            </head>
            <body>
                <p>Extracting tools for %s, may take up to a minute ...</p>
            </body>
        </html>
        """ % name).encode('UTF-8')

        self.send_response(200, "OK")
        self.send_header("Content-Type", "text/html;charset=UTF-8")
        self.send_header("Content-Length", str(len(content)))
        self.send_header("Retry-After", "5")
        self.end_headers()
        self.wfile.write(content)

    def _get_extension(self, namespace):
        index = namespace.find('-')
        if index == -1:
            return ''
        return namespace[index::]

    def do_GET(self):
        path = self.path.strip("/")
        segments = path.split("/")
        extension = self._get_extension(RELEASE_NAMESPACE)

        if len(segments) == 1 and re.match('[0-9]+[a-zA-Z0-9.\-]+[a-zA-Z0-9]', segments[0]):
            name = segments[0]

            release_imagestream_name = 'release'
            if '-ec.' in name:
                release_imagestream_name = '4-dev-preview'

            if os.path.isfile(os.path.join(name, "sha256sum.txt")) or os.path.isfile(os.path.join(name, "FAILED.md")):
                handler.do_GET(self)
                return

            if os.path.isfile(os.path.join(name, "DOWNLOADING.md")):
                if check_stale_download(path):
                    os.remove(os.path.join(name, "DOWNLOADING.md"))
                self._present_default_content(name)
                return

            try:
                os.mkdir(name)
            except OSError:
                pass

            with open(os.path.join(name, "DOWNLOADING.md"), "w") as outfile:
                outfile.write("Downloading %s" % name)

            try:
                self._present_default_content(name)
                self.wfile.flush()

                subprocess.check_output(["oc", "adm", "release", "extract", "--tools", "--to", name, "--command-os", "*", "%s/%s/%s%s:%s" % (REGISTRY, RELEASE_NAMESPACE, release_imagestream_name, extension, name)],
                                        stderr=subprocess.STDOUT)
                os.remove(os.path.join(name, "DOWNLOADING.md"))

            except CalledProcessError as e:
                print("Unable to get release tools for %s: %s" % (name, e.output))

                if e.output and (b"no such image" in e.output or
                                 b"image does not exist" in e.output or
                                 (b"error: image" in e.output and b"does not exist" in e.output) or
                                 b"unauthorized: access to the requested resource is not authorized" in e.output or
                                 b"some required images are missing" in e.output or
                                 b"invalid reference format" in e.output):
                    with open(os.path.join(name, "FAILED.md"), "w") as outfile:
                        outfile.write("Unable to get release tools: %s" % e.output)
                    os.remove(os.path.join(name, "DOWNLOADING.md"))
                    return

                with open(os.path.join(name, "DOWNLOADING.md"), "w") as outfile:
                    outfile.write("Unable to get release tools: %s" % e.output)

            except Exception as e:
                print("Unable to get release tools for %s: %s" % (name, e.message))
                self.log_error('An unexpected error has occurred: {}'.format(e.message))

            return

        handler.do_GET(self)

# Create socket
addr = ('', 8080)
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(addr)
sock.listen(5)

# Launch multiple listeners as threads
class Thread(threading.Thread):
  def __init__(self, i):
    threading.Thread.__init__(self)
    self.i = i
    self.daemon = True
    self.start()
  def run(self):
    with socketserver.TCPServer(addr, FileServer, False) as httpd:
      # Prevent the HTTP server from re-binding every handler.
      # https://stackoverflow.com/questions/46210672/
      httpd.socket = sock
      httpd.server_bind = self.server_close = lambda self: None
      httpd.serve_forever()
[Thread(i) for i in range(100)]
time.sleep(9e9)
END
  python3 /tmp/serve.py
else
  cat <<END >/tmp/serve.py
import re, os, subprocess, time, threading, socket, BaseHTTPServer, SimpleHTTPServer, glob
from subprocess import CalledProcessError

RELEASE_NAMESPACE = os.getenv('RELEASE_NAMESPACE', 'ocp')
REGISTRY = os.getenv('REGISTRY', 'registry.ci.openshift.org')

handler = SimpleHTTPServer.SimpleHTTPRequestHandler


files = glob.glob('/srv/cache/**/DOWNLOADING.md')
for file in files:
    print("Removing: %s" % file)
    os.remove(file)


def check_stale_download(path):
    service_list = subprocess.Popen(['ps', '-ef'], stdout=subprocess.PIPE)
    grep_command = '[o]c adm release extract --tools --to %s' % path
    filtered_list = subprocess.Popen(['grep', grep_command],
                                     stdin=service_list.stdout,
                                     stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    service_list.stdout.close()
    out, err = filtered_list.communicate()
    if out.decode("utf-8") != '' and err.decode("utf-8") == '':
        return False
    return True


class FileServer(handler):
    def _present_default_content(self, name):
        content = ("""<!DOCTYPE html>
        <html>
            <head>
                <meta http-equiv=\"refresh\" content=\"5\">
            </head>
            <body>
                <p>Extracting tools for %s, may take up to a minute ...</p>
            </body>
        </html>
        """ % name).encode('UTF-8')

        self.send_response(200, "OK")
        self.send_header("Content-Type", "text/html;charset=UTF-8")
        self.send_header("Content-Length", str(len(content)))
        self.send_header("Retry-After", "5")
        self.end_headers()
        self.wfile.write(content)
        self.wfile.close()

    def _get_extension(self, namespace):
        index = namespace.find('-')
        if index == -1:
            return ''
        return namespace[index::]

    def do_GET(self):
        path = self.path.strip("/")
        segments = path.split("/")
        extension = self._get_extension(RELEASE_NAMESPACE)

        if len(segments) == 1 and re.match('[0-9]+[a-zA-Z0-9.\-]+[a-zA-Z0-9]', segments[0]):
            name = segments[0]

            if os.path.isfile(os.path.join(name, "sha256sum.txt")) or os.path.isfile(os.path.join(name, "FAILED.md")):
                handler.do_GET(self)
                return

            if os.path.isfile(os.path.join(name, "DOWNLOADING.md")):
                if check_stale_download(path):
                    os.remove(os.path.join(name, "DOWNLOADING.md"))
                self._present_default_content(name)
                return

            try:
                os.mkdir(name)
            except OSError:
                pass

            with open(os.path.join(name, "DOWNLOADING.md"), "w") as outfile:
                outfile.write("Downloading %s" % name)

            try:
                self._present_default_content(name)

                subprocess.check_output(["oc", "adm", "release", "extract", "--tools", "--to", name, "--command-os", "*", "%s/%s/release%s:%s" % (REGISTRY, RELEASE_NAMESPACE, extension, name)],
                                        stderr=subprocess.STDOUT)
                os.remove(os.path.join(name, "DOWNLOADING.md"))

            except CalledProcessError as e:
                if e.output and ("no such image" in e.output or
                                  "image does not exist" in e.output or
                                  "unauthorized: access to the requested resource is not authorized" in e.output or
                                  "some required images are missing" in e.output or
                                  "invalid reference format" in e.output):
                    with open(os.path.join(name, "FAILED.md"), "w") as outfile:
                        outfile.write("Unable to get release tools: %s" % e.output)
                    os.remove(os.path.join(name, "DOWNLOADING.md"))
                    return

                with open(os.path.join(name, "DOWNLOADING.md"), "w") as outfile:
                    outfile.write("Unable to get release tools: %s" % e.output)

            except Exception as e:
                self.log_error('An unexpected error has occurred: {}'.format(e.message))

            return

        handler.do_GET(self)

# Create socket
addr = ('', 8080)
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(addr)
sock.listen(5)

# Launch multiple listeners as threads
class Thread(threading.Thread):
    def __init__(self, index):
        threading.Thread.__init__(self)
        self.i = index
        self.daemon = True
        self.start()

    def run(self):
        server = FileServer
        server.extensions_map = {".md": "text/plain", ".asc": "text/plain", ".txt": "text/plain", "": "application/octet-stream"}
        httpd = BaseHTTPServer.HTTPServer(addr, server, False)

        # Prevent the HTTP server from re-binding every handler.
        # https://stackoverflow.com/questions/46210672/
        httpd.socket = sock
        httpd.server_bind = self.server_close = lambda self: None

        httpd.serve_forever()


[Thread(i) for i in range(100)]
time.sleep(9e9)
END
  python /tmp/serve.py
fi
              `,
						},
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						}},
						ReadinessProbe: probe,
						LivenessProbe:  probe,
					},
				},
			},
		},
	}
	return spec
}
