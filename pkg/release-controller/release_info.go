package releasecontroller

import (
	"bytes"
	"context"
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	jiraBaseClient "github.com/andygrunwald/go-jira"
	"github.com/golang/groupcache"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/patrickmn/go-cache"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/prow/pkg/jira"
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

const maxChunkSize = 450 // this seems to be the maximum Jira can handle, currently

const coreosExtensionsMetadataPath = "usr/share/rpm-ostree/extensions.json"

var (
	ocPath = ""
)

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
		case "rpmdiff":
			if strings.Contains(parts[1], "\x00") || strings.Contains(parts[2], "\x00") {
				s, err = "", fmt.Errorf("invalid from/to")
			} else {
				var rpmdiff RpmDiff
				rpmdiff, err = info.RpmDiff(parts[1], parts[2])
				if err == nil {
					var rpmdiffByte []byte
					rpmdiffByte, err = json.Marshal(rpmdiff)
					if err != nil {
						klog.V(4).Infof("Failed to Marshal Rpm Diff; from: %s to: %s; %s", parts[1], parts[2], err)
					} else {
						s = string(rpmdiffByte)
					}
				}
			}
		case "rpmlist":
			var rpmlist RpmList
			rpmlist, err = info.RpmList(parts[1])
			if err == nil {
				var rpmlistByte []byte
				rpmlistByte, err = json.Marshal(rpmlist)
				if err != nil {
					klog.V(4).Infof("Failed to Marshal Rpm List for %s; %s", parts[1], err)
				} else {
					s = string(rpmlistByte)
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
		return sink.SetString(s)
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

func (c *CachingReleaseInfo) RpmDiff(from, to string) (RpmDiff, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"rpmdiff", from, to}, "\x00"), groupcache.StringSink(&s))
	if err != nil {
		return RpmDiff{}, err
	}
	if s == "" {
		return RpmDiff{}, nil
	}
	var rpmdiff RpmDiff
	if err = json.Unmarshal([]byte(s), &rpmdiff); err != nil {
		return RpmDiff{}, err
	}
	return rpmdiff, nil
}

func (c *CachingReleaseInfo) RpmList(image string) (RpmList, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"rpmlist", image}, "\x00"), groupcache.StringSink(&s))
	if err != nil {
		return RpmList{}, err
	}
	if s == "" {
		return RpmList{}, nil
	}
	var rpmlist RpmList
	if err = json.Unmarshal([]byte(s), &rpmlist); err != nil {
		return RpmList{}, err
	}
	return rpmlist, nil
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
	RpmList(image string) (RpmList, error)
	RpmDiff(from, to string) (RpmDiff, error)
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
	jiraCache   *cache.Cache
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
		jiraCache:   cache.New(24*time.Hour, 1*time.Hour),
	}
}

func ocCmd(args ...string) ([]byte, []byte, error) {
	return ocCmdExt("", args...)
}

func ocCmdExt(dir string, args ...string) ([]byte, []byte, error) {
	var err error

	if ocPath == "" {
		ocPath, err = exec.LookPath("oc")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to find `oc` executable: %w", err)
		}
	}

	cmd := exec.Command(ocPath, args...)
	klog.V(4).Infof("Running oc command: %s", cmd.String())

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stdin = nil
	cmd.Stderr = errOut

	if dir != "" {
		cmd.Dir = dir
	}

	if err := cmd.Run(); err != nil {
		klog.V(4).Infof("Failed oc command: %v\n$ %s\n%s\n%s", err, cmd.String(), errOut.String(), out.String())
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return nil, nil, fmt.Errorf("could not run oc command: %v", msg)
	}
	klog.V(4).Infof("Finished oc command: %s", cmd.String())

	return out.Bytes(), errOut.Bytes(), nil
}

func (r *ExecReleaseInfo) ReleaseInfo(image string) (string, error) {
	if _, err := imagereference.Parse(image); err != nil {
		return "", fmt.Errorf("%s is not an image reference: %v", image, err)
	}

	info, _, err := ocCmd("adm", "release", "info", "-o", "json", image)
	if err != nil {
		return "", fmt.Errorf("could not get release info for %s: %v", image, err)
	}
	return string(info), nil
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

	// XXX: switch to ocCmd()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	oc, err := exec.LookPath("oc")
	if err != nil {
		return "", fmt.Errorf("failed to find `oc` executable: %w", err)
	}
	args := []string{"adm", "release", "info", "--changelog=/tmp/git/", from, to}
	if isJson {
		args = append(args, "--output=json")
	}
	cmd := exec.Command(oc, args...)
	klog.V(4).Infof("Running changelog command: %s", cmd.String())
	cmd.Stdout = out
	cmd.Stdin = nil
	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		if strings.Contains(msg, "Could not load commits for") {
			klog.Warningf("Generated changelog with missing commits:\n$ %s\n%s", cmd.String(), errOut.String())
		} else {
			klog.V(4).Infof("Failed to generate changelog: %v\n$ %s\n%s\n%s", err, cmd.String(), errOut.String(), out.String())
			return "", fmt.Errorf("could not generate a changelog: %v", msg)
		}
	}
	klog.V(4).Infof("Finished running changelog command: %s", cmd.String())
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

	// XXX: switch to ocCmd()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	oc, err := exec.LookPath("oc")
	if err != nil {
		return nil, fmt.Errorf("failed to find `oc` executable: %w", err)
	}
	cmd := exec.Command(oc, "adm", "release", "info", "--bugs=/tmp/git/", "--output=json", "--skip-bug-check", from, to)
	cmd.Stdout = out
	cmd.Stdin = nil
	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		klog.V(4).Infof("Failed to generate bug list: %v\n$ %s\n%s\n%s", err, cmd.String(), errOut.String(), out.String())
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

func (r *ExecReleaseInfo) RpmList(image string) (RpmList, error) {
	if _, err := imagereference.Parse(image); err != nil {
		return RpmList{}, fmt.Errorf("%s is not an image reference: %v", image, err)
	}
	if strings.HasPrefix(image, "-") {
		return RpmList{}, fmt.Errorf("not a valid reference")
	}

	var rpmlist RpmList

	out, _, err := ocCmd("adm", "release", "info", "--rpmdb-cache=/tmp/rpmdb/", "--output=json", "--rpmdb", image)
	if err != nil {
		return RpmList{}, fmt.Errorf("failed to query RPM list for %s", image)
	}
	if err = json.Unmarshal(out, &rpmlist.Packages); err != nil {
		return RpmList{}, fmt.Errorf("unmarshaling RPM list: %s", err)
	}

	// XXX: This is hacky... honestly we should just have consistent tag names
	// across OCP and OKD. But for this specific case, we should probably have e.g.
	// an explicit component version string instead on the extensions container that
	// identifies it as such so we don't have to hardcode any tag names here
	extensionsTagName := "rhel-coreos-extensions"
	if _, ok := rpmlist.Packages["centos-stream-release"]; ok {
		extensionsTagName = "stream-coreos-extensions"
	}

	out, _, err = ocCmd("adm", "release", "info", "--image-for", extensionsTagName, image)
	if err != nil {
		return RpmList{}, fmt.Errorf("failed to query RPM extensions image for %s", image)
	}
	extensionsImage := strings.TrimSpace(string(out))

	tmpdir, err := os.MkdirTemp("", "extensions")
	if err != nil {
		return RpmList{}, fmt.Errorf("failed to create tmpdir for RPM extensions list for %s", image)
	}

	defer os.RemoveAll(tmpdir)
	// see https://github.com/openshift/os/commit/31816acb1ae377c9c48f1e4bc70fbf63cf4adc2d
	_, _, err = ocCmdExt(tmpdir, "image", "extract", extensionsImage+"[-1]", "--file", coreosExtensionsMetadataPath)
	if err != nil {
		return RpmList{}, fmt.Errorf("failed to query RPM extensions list for %s", image)
	}
	extensions, err := os.ReadFile(filepath.Join(tmpdir, "extensions.json"))
	if err != nil {
		// Let's not break package lists/diffs if we fail to get extensions.
		// Early 4.19 builds didn't have the required metadata yet.
		klog.Warningf("Continuing without extensions information: %v\n", err)
		return rpmlist, nil
	}
	if err = json.Unmarshal(extensions, &rpmlist.Extensions); err != nil {
		return RpmList{}, fmt.Errorf("unmarshaling extensions: %s", err)
	}

	return rpmlist, nil
}

func (r *ExecReleaseInfo) RpmDiff(from, to string) (RpmDiff, error) {
	if _, err := imagereference.Parse(from); err != nil {
		return RpmDiff{}, fmt.Errorf("%s is not an image reference: %v", from, err)
	}
	if _, err := imagereference.Parse(to); err != nil {
		return RpmDiff{}, fmt.Errorf("%s is not an image reference: %v", to, err)
	}
	if strings.HasPrefix(from, "-") || strings.HasPrefix(to, "-") {
		return RpmDiff{}, fmt.Errorf("not a valid reference")
	}

	out, _, err := ocCmd("adm", "release", "info", "--rpmdb-cache=/tmp/rpmdb/", "--output=json", "--rpmdb-diff", from, to)
	if err != nil {
		return RpmDiff{}, fmt.Errorf("could not generate RPM diff for %s to %s: %v", from, to, err)
	}

	var rpmdiff RpmDiff
	if err = json.Unmarshal(out, &rpmdiff); err != nil {
		return RpmDiff{}, fmt.Errorf("unmarshaling RPM diff: %s", err)
	}

	return rpmdiff, nil
}

type RpmList struct {
	Packages   map[string]string `json:"packages"`
	Extensions map[string]string `json:"extensions"`
}

type RpmDiff struct {
	Changed map[string]RpmChangedDiff `json:"changed,omitempty"`
	Added   map[string]string         `json:"added,omitempty"`
	Removed map[string]string         `json:"removed,omitempty"`
}

type RpmChangedDiff struct {
	Old string `json:"old,omitempty"`
	New string `json:"new,omitempty"`
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

	// XXX: switch to ocCmd()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	oc, err := exec.LookPath("oc")
	if err != nil {
		return "", fmt.Errorf("failed to find `oc` executable: %w", err)
	}
	cmd := exec.Command(oc, "image", "info", "--filter-by-os", fmt.Sprintf("linux/%s", architecture), "-o", "json", image)
	cmd.Stdout = out
	cmd.Stdin = nil
	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		klog.V(4).Infof("Failed to get image info for %s: %v\n$ %s\n%s\n%s", image, err, cmd.String(), errOut.String(), out.String())
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

	var listOfAllIssues []string
	for _, issue := range issues {
		listOfAllIssues = append(listOfAllIssues, issue.ID)
	}
	issuesWithDemoLink, err := r.GetIssuesWithDemoLink(listOfAllIssues)
	var issuesWithDemoLinkList []string
	for _, issue := range issuesWithDemoLink {
		issuesWithDemoLinkList = append(issuesWithDemoLinkList, issue.ID)
	}
	if err != nil {
		return "", err
	}
	issuesWithRemoteLinkDetails, err := r.GetRemoteLinksWithConcurrency(issuesWithDemoLinkList, 1*time.Second)
	if err != nil {
		return "", err
	}

	t := TransformJiraIssues(issues, issuesToPRMap, issuesWithRemoteLinkDetails)
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
					case map[string]any:
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
	Demos          []string
	ResolutionDate time.Time
	Transitions    []Transition
}

func typeCheck(o any) string {
	switch v := o.(type) {
	case string:
		return v
	case map[string]any:
		return typeCheck(v["key"])
	default:
		klog.Warningf("unable to parse the Jira unknown field: %v\n", o)
		return ""
	}
}

func TransformJiraIssues(issues []jiraBaseClient.Issue, prMap map[string][]string, demoURLsMap map[string][]string) map[string]IssueDetails {
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
		demoLinks := demoURLsMap[issue.ID]

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
			Demos:          demoLinks,
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

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for _, feature := range featuresList {
		wg.Add(1)
		limit <- struct{}{}

		<-ticker.C

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

func (r *ExecReleaseInfo) divideSlice(issues []string, chunk int, skipCache bool) ([][]string, []jiraBaseClient.Issue) {

	var result []jiraBaseClient.Issue
	uncashedIssues := make([]string, 0)

	if !skipCache {
		for _, issue := range issues {
			cashedIssue, found := r.jiraCache.Get(issue)
			if found {
				result = append(result, cashedIssue.(jiraBaseClient.Issue))
			} else {
				uncashedIssues = append(uncashedIssues, issue)
			}
		}
	} else {
		uncashedIssues = issues
	}

	var divided [][]string
	for index := 0; index < len(uncashedIssues); index += chunk {
		end := min(index+chunk, len(issues))
		divided = append(divided, issues[index:end])
	}
	return divided, result
}

func (r *ExecReleaseInfo) GetIssuesWithChunks(issues []string) (result []jiraBaseClient.Issue, err error) {
	// This will prevent a Panic if/when the release-controller's are run without the necessary jira flags
	if r.jiraClient == nil {
		return result, fmt.Errorf("unable to communicate with Jira")
	}

	chunk := maxChunkSize

	// Divide issues into chunks
	dividedIssues, cashedIssues := r.divideSlice(issues, chunk, false)
	result = append(result, cashedIssues...)

	// Search for issues in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	var buf bytes.Buffer
	var invalidIDs []string

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for _, parts := range dividedIssues {
		wg.Add(1)
		jql := fmt.Sprintf("id IN (%s)", strings.Join(parts, ","))

		<-ticker.C

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
				if jiraErr, ok := err.(*jira.JiraError); ok {
					if jiraOriginalErr, ok := jiraErr.OriginalError.(*jiraBaseClient.Error); ok {
						for _, id := range parts {
							for _, errorMessage := range jiraOriginalErr.ErrorMessages {
								if strings.Contains(errorMessage, fmt.Sprintf("'%s'", id)) {
									invalidIDs = append(invalidIDs, id)
								}
							}
						}

					}
				}
				if len(invalidIDs) > 0 {
					filtered := make([]string, 0)
					excludeMap := make(map[string]bool)
					for _, item := range invalidIDs {
						excludeMap[item] = true
					}
					for _, item := range parts {
						if !excludeMap[item] {
							filtered = append(filtered, item)
						}
					}
					validResults, validErr := r.GetIssuesWithChunks(filtered)
					if validErr != nil {
						buf.WriteString(validErr.Error() + "\n")
					}
					result = append(result, validResults...)

				} else {
					err = fmt.Errorf("search failed: %w", err)
					buf.WriteString(err.Error() + "\n")
				}
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, issue := range issues {
				r.jiraCache.Set(issue.Key, issue, cache.DefaultExpiration)
			}
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

func (r *ExecReleaseInfo) GetIssuesWithDemoLink(issues []string) (result []jiraBaseClient.Issue, err error) {
	// This will prevent a Panic if/when the release-controller's are run without the necessary jira flags
	if r.jiraClient == nil {
		return result, fmt.Errorf("unable to communicate with Jira")
	}

	chunk := maxChunkSize

	dividedIssues, _ := r.divideSlice(issues, chunk, true)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var buf bytes.Buffer

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for _, parts := range dividedIssues {
		wg.Add(1)
		jql := fmt.Sprintf("issueFunction in linkedIssuesOfremote(\"demo\") AND id IN (%s)", strings.Join(parts, ","))

		<-ticker.C

		go func(jql string) {
			defer wg.Done()
			issues, _, err := r.jiraClient.SearchWithContext(
				context.Background(),
				jql,
				&jiraBaseClient.SearchOptions{
					MaxResults: chunk + 1,
				},
			)
			if err != nil {
				mu.Lock()
				defer mu.Unlock()
				err = fmt.Errorf("search failed: %w", err)
				buf.WriteString(err.Error() + "\n")
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

func (r *ExecReleaseInfo) GetRemoteLinksWithConcurrency(issues []string, requestInterval time.Duration) (map[string][]string, error) {
	// Prevent Panic if Jira client is not set
	if r.jiraClient == nil {
		return nil, fmt.Errorf("unable to communicate with Jira")
	}

	var (
		mapIssueDemoLink = make(map[string][]string)
		wg               sync.WaitGroup
		mu               sync.Mutex
		workers          = make(chan struct{}, 5) // it does not make sense to have more workers since the API rate is limited
		ticker           = time.NewTicker(requestInterval)
		errorBuilder     strings.Builder
		demoRegex        = regexp.MustCompile(`\bdemo\b`) // Precompile regex
	)

	defer ticker.Stop()

	worker := func(issue string) {
		defer wg.Done()
		<-ticker.C // Wait for API call slot

		var links []jiraBaseClient.RemoteLink
		issueRemoteLinkKey := fmt.Sprintf("%s_remote_link", issue)

		if cachedLinks, found := r.jiraCache.Get(issueRemoteLinkKey); found {
			links = cachedLinks.([]jiraBaseClient.RemoteLink)
		} else {
			var err error
			links, err = r.jiraClient.GetRemoteLinks(issue)
			if err != nil {
				mu.Lock()
				errorBuilder.WriteString(fmt.Sprintf("search failed for issue %s: %v\n", issue, err))
				mu.Unlock()
				return
			}
			r.jiraCache.Set(issueRemoteLinkKey, links, cache.DefaultExpiration)
		}

		// Process links
		var linkUrls []string
		for _, link := range links {
			if demoRegex.MatchString(strings.ToLower(link.Object.Title)) {
				linkUrls = append(linkUrls, link.Object.URL)
			}
		}

		// Store results
		mu.Lock()
		mapIssueDemoLink[issue] = linkUrls
		mu.Unlock()
	}

	for _, issue := range issues {
		wg.Add(1)
		workers <- struct{}{} // Acquire worker slot

		go func(issue string) {
			<-workers // Release worker slot immediately
			worker(issue)
		}(issue)
	}

	wg.Wait()
	close(workers)

	if errorBuilder.Len() > 0 {
		return mapIssueDemoLink, stdErrors.New(errorBuilder.String())
	}

	return mapIssueDemoLink, nil
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

cat <<END >/tmp/serve.py
import glob, html, http.server, io, os, re, socket, socketserver, subprocess, sys, threading, time, urllib.parse
from http import HTTPStatus
from subprocess import CalledProcessError

handler = http.server.SimpleHTTPRequestHandler

CACHE_DIR = '/srv/cache'
RELEASE_NAMESPACE = os.getenv('RELEASE_NAMESPACE', 'ocp')
REGISTRY = os.getenv('REGISTRY', 'registry.ci.openshift.org')

files = glob.glob(f'{CACHE_DIR}/**/DOWNLOADING.md', recursive=True)
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


def get_extension(namespace):
    index = namespace.find('-')
    if index == -1:
        return ''
    return namespace[index::]


class FileServer(handler):
    def _present_default_content(self, name):
        content = ("""<!DOCTYPE html>
        <html>
            <head>
                <meta http-equiv=\"refresh\" content=\"5\">
            </head>
            <body>
                <p>Extracting tools for %s, this may take a few minutes...</p>
            </body>
        </html>
        """ % name).encode('UTF-8')

        self.send_response(200, "OK")
        self.send_header("Content-Type", "text/html;charset=UTF-8")
        self.send_header("Content-Length", str(len(content)))
        self.send_header("Retry-After", "5")
        self.end_headers()
        self.wfile.write(content)

    def _present_blocked_content(self, name):
        content = ("""<!DOCTYPE html>
        <html>
            <head>
            <body>
                <p>You are attempting to download an Openshift release (%s) that has past it's end of life and is no longer available via this utility!</br>Please contact us in: <a href="https://redhat.enterprise.slack.com/archives/CNHC2DK2M">#forum-ocp-crt</a> if this is a problem.</br>Thank you!</p>
            </body>
        </html>
        """ % name).encode('UTF-8')

        self.send_response(200, "OK")
        self.send_header("Content-Type", "text/html;charset=UTF-8")
        self.send_header("Content-Length", str(len(content)))
        self.end_headers()
        self.wfile.write(content)

    def list_directory(self, path):
        try:
            list = os.listdir(path)
        except OSError:
            self.send_error(
                HTTPStatus.NOT_FOUND,
                "No permission to list directory")
            return None
        list.sort(key=lambda a: a.lower())
        r = []
        try:
            displaypath = urllib.parse.unquote(self.path,
                                               errors='surrogatepass')
        except UnicodeDecodeError:
            displaypath = urllib.parse.unquote(self.path)
        displaypath = html.escape(displaypath, quote=False)
        enc = sys.getfilesystemencoding()
        title = f'Directory listing for {displaypath}'
        r.append('<!DOCTYPE HTML>')
        r.append('<html lang="en">')
        r.append('<head>')
        r.append(f'<meta charset="{enc}">')
        r.append(f'<title>{title}</title>\n</head>')
        r.append(f'<body>\n<h1 style="color:red">If you\'re seeing this message, we\'d love to hear from you...</br>If you rely on this functionality for your daily work, please reach out to us in: <a href="https://redhat.enterprise.slack.com/archives/CNHC2DK2M">#forum-ocp-crt</a></h1>')
        r.append(f'\n<h1>{title}</h1>')
        r.append('<hr>\n<ul>')
        for name in list:
            fullname = os.path.join(path, name)
            displayname = linkname = name
            # Append / for directories or @ for symbolic links
            if os.path.isdir(fullname):
                displayname = name + "/"
                linkname = name + "/"
            if os.path.islink(fullname):
                displayname = name + "@"
                # Note: a link to a directory displays with @ and links with /
            r.append('<li><a href="%s">%s</a></li>'
                    % (urllib.parse.quote(linkname,
                                          errors='surrogatepass'),
                       html.escape(displayname, quote=False)))
        r.append('</ul>\n<hr>\n</body>\n</html>\n')
        encoded = '\n'.join(r).encode(enc, 'surrogateescape')
        f = io.BytesIO()
        f.write(encoded)
        f.seek(0)
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-type", "text/html; charset=%s" % enc)
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        return f

    def do_GET(self):
        path = self.path.strip("/")
        segments = path.split("/")
        extension = get_extension(RELEASE_NAMESPACE)

        if len(segments) == 1 and re.match('[0-9]+[a-zA-Z0-9.-]+[a-zA-Z0-9]', segments[0]):
            release = segments[0]

            parts = release.split('.')
            if len(parts) < 2 or int(parts[1]) < 11:
                self._present_blocked_content(release)
                return
            
            release_cache_dir = os.path.join(CACHE_DIR, release)

            release_imagestream_name = 'release'
            if '-ec.' in release:
                release_imagestream_name = '4-dev-preview'

            if os.path.isfile(os.path.join(release_cache_dir, "sha256sum.txt")) or os.path.isfile(os.path.join(release_cache_dir, "FAILED.md")):
                handler.do_GET(self)
                return

            if os.path.isfile(os.path.join(release_cache_dir, "DOWNLOADING.md")):
                if check_stale_download(release_cache_dir):
                    os.remove(os.path.join(release_cache_dir, "DOWNLOADING.md"))
                self._present_default_content(release)
                return

            try:
                os.mkdir(release_cache_dir)
            except OSError:
                pass

            with open(os.path.join(release_cache_dir, "DOWNLOADING.md"), "w") as outfile:
                outfile.write("Downloading %s" % release)

            try:
                self._present_default_content(release)
                self.wfile.flush()

                subprocess.check_output(["oc", "adm", "release", "extract", "--tools", "--to", release_cache_dir, "--command-os", "*",
                                         "%s/%s/%s%s:%s" % (REGISTRY, RELEASE_NAMESPACE, release_imagestream_name, extension, release)],
                                        stderr=subprocess.STDOUT)

            except CalledProcessError as e:
                print(f'Unable to get release tools for {release}: {e.output}')
                with open(os.path.join(release_cache_dir, "FAILED.md"), "w") as outfile:
                    outfile.write(f'Unable to get release tools for {release}: {e.output}')

            except Exception as e:
                print(f'An unexpected error has occurred: {e}')
                with open(os.path.join(release_cache_dir, "FAILED.md"), "w") as outfile:
                    outfile.write(f'An unexpected error has occurred: {e}')
                self.log_error(f'An unexpected error has occurred: {e}')

            finally:
                os.remove(os.path.join(release_cache_dir, "DOWNLOADING.md"))

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
