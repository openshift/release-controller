package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/golang/glog"
	"github.com/golang/groupcache"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	imagereference "github.com/openshift/library-go/pkg/image/reference"
)

type CachingReleaseInfo struct {
	cache *groupcache.Group
}

func NewCachingReleaseInfo(info ReleaseInfo, size int64) ReleaseInfo {
	cache := groupcache.NewGroup("release", size, groupcache.GetterFunc(func(ctx groupcache.Context, key string, sink groupcache.Sink) error {
		var s string
		var err error
		parts := strings.Split(key, "\x00")
		switch parts[0] {
		case "changelog":
			s, err = info.ChangeLog(parts[1], parts[2])
		case "releaseinfo":
			s, err = info.ReleaseInfo(parts[1])
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

func (c *CachingReleaseInfo) ChangeLog(from, to string) (string, error) {
	if strings.Contains(from, "\x00") || strings.Contains(to, "\x00") {
		return "", fmt.Errorf("invalid from/to")
	}
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"changelog", from, to}, "\x00"), groupcache.StringSink(&s))
	return s, err
}

func (c *CachingReleaseInfo) ReleaseInfo(image string) (string, error) {
	var s string
	err := c.cache.Get(context.TODO(), strings.Join([]string{"releaseinfo", image}, "\x00"), groupcache.StringSink(&s))
	return s, err
}

type ReleaseInfo interface {
	ChangeLog(from, to string) (string, error)
	ReleaseInfo(image string) (string, error)
}

type ExecReleaseInfo struct {
	client      kubernetes.Interface
	restConfig  *rest.Config
	namespace   string
	name        string
	imageNameFn func() (string, error)
}

// NewExecReleaseInfo creates a stateful set, in the specified namespace, that provides git changelogs to the
// Release Status website.  The provided name will prevent other instances of the stateful set
// from being created when created with an identical name.
func NewExecReleaseInfo(client kubernetes.Interface, restConfig *rest.Config, namespace string, name string, imageNameFn func() (string, error)) *ExecReleaseInfo {
	return &ExecReleaseInfo{
		client:      client,
		restConfig:  restConfig,
		namespace:   namespace,
		name:        name,
		imageNameFn: imageNameFn,
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
		glog.V(4).Infof("Failed to get release info for %s: %v\n$ %s\n%s\n%s", image, err, strings.Join(cmd, " "), errOut.String(), out.String())
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return "", fmt.Errorf("could not get release info for %s: %v", image, msg)
	}
	return out.String(), nil
}

func (r *ExecReleaseInfo) ChangeLog(from, to string) (string, error) {
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
		glog.V(4).Infof("Failed to generate changelog: %v\n$ %s\n%s\n%s", err, strings.Join(cmd, " "), errOut.String(), out.String())
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return "", fmt.Errorf("could not generate a changelog: %v", msg)
	}

	return out.String(), nil
}

func (r *ExecReleaseInfo) refreshPod() error {
	sts, err := r.client.AppsV1().StatefulSets(r.namespace).Get("git-cache", metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		sts = nil
	}

	if sts != nil && len(sts.Annotations["release-owner"]) > 0 && sts.Annotations["release-owner"] != r.name {
		glog.Infof("Another release controller is managing git-cache, ignoring")
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
		if _, err := r.client.AppsV1().StatefulSets(r.namespace).Create(sts); err != nil {
			return fmt.Errorf("can't create stateful set for cache: %v", err)
		}
		return nil
	}

	sts.Spec = spec
	if _, err := r.client.AppsV1().StatefulSets(r.namespace).Update(sts); err != nil {
		return fmt.Errorf("can't update stateful set for cache: %v", err)
	}
	return nil
}

func (r *ExecReleaseInfo) specHash(image string) appsv1.StatefulSetSpec {
	spec := appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "git-cache",
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
							"storage": resource.MustParse("40Gi"),
						},
					},
				},
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app": "git-cache",
					"release": r.name,
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "git-credentials", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "git-credentials"}}},
				},
				Containers: []corev1.Container{
					{
						Name:  "git",
						Image: image,
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: "/tmp"},
							{Name: "GIT_COMMITTER_NAME", Value: "test"},
							{Name: "GIT_COMMITTER_EMAIL", Value: "test@test.com"},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "git", MountPath: "/tmp/git/"},
							{Name: "git-credentials", MountPath: "/tmp/.git-credentials", SubPath: ".git-credentials"},
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

              git config --global credential.helper store
              git config --global user.name test
              git config --global user.email test@test.com
              oc registry login
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
	client      		kubernetes.Interface
	restConfig  		*rest.Config
	namespace   		string
	name        		string
	releaseNamespace	string
	imageNameFn 		func() (string, error)
}

// NewExecReleaseFiles creates a stateful set, in the specified namespace, that provides cached access to downloaded
//installer images from the Release Status website.  The provided name will prevent other instances of the stateful set
// from being created when created with an identical name.  The releaseNamespace is used to ensure that the tools are
// downloaded from the correct namespace.
func NewExecReleaseFiles(client kubernetes.Interface, restConfig *rest.Config, namespace string, name string, releaseNamespace string, imageNameFn func() (string, error)) *ExecReleaseFiles {
	return &ExecReleaseFiles{
		client:      		client,
		restConfig:  		restConfig,
		namespace:   		namespace,
		name:        		name,
		releaseNamespace:	releaseNamespace,
		imageNameFn: 		imageNameFn,
	}
}

func (r *ExecReleaseFiles) refreshPod() error {
	sts, err := r.client.AppsV1().StatefulSets(r.namespace).Get("files-cache", metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		sts = nil
	}

	if sts != nil && len(sts.Annotations["release-owner"]) > 0 && sts.Annotations["release-owner"] != r.name {
		glog.Infof("Another release controller is managing git-cache, ignoring")
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
		if _, err := r.client.AppsV1().StatefulSets(r.namespace).Create(sts); err != nil {
			return fmt.Errorf("can't create stateful set for cache: %v", err)
		}
		return nil
	}

	sts.Spec = spec
	if _, err := r.client.AppsV1().StatefulSets(r.namespace).Update(sts); err != nil {
		return fmt.Errorf("can't update stateful set for cache: %v", err)
	}
	return nil
}

func (r *ExecReleaseFiles) specHash(image string) appsv1.StatefulSetSpec {
	one := int64(1)

	probe := &corev1.Probe{
		Handler: corev1.Handler{
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
							{Name: "RELEASE_NAMESPACE", Value: r.releaseNamespace},
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
mkdir -p /tmp/.docker/
cp /tmp/pull-secret/* /tmp/.docker/ || true
oc registry login

cat <<END >>/tmp/serve.py
import re
import os
import subprocess
import time
import threading
import socket
import BaseHTTPServer
import SimpleHTTPServer

from subprocess import CalledProcessError

# Create socket
addr = ('', 8080)
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(addr)
sock.listen(5)

RELEASE_NAMESPACE = os.getenv('RELEASE_NAMESPACE', 'ocp')


class FileServer(SimpleHTTPServer.SimpleHTTPRequestHandler):
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

    def do_GET(self):
        path = self.path.strip("/")
        segments = path.split("/")

        if len(segments) == 1 and re.match('[0-9]+[a-zA-Z0-9.\-]+[a-zA-Z0-9]', segments[0]):
            name = segments[0]

            if os.path.isfile(os.path.join(name, "sha256sum.txt")) or os.path.isfile(os.path.join(name, "FAILED.md")):
                SimpleHTTPServer.SimpleHTTPRequestHandler.do_GET(self)
                return

            if os.path.isfile(os.path.join(name, "DOWNLOADING.md")):
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

                subprocess.check_output(["oc", "adm", "release", "extract", "--tools", "--to", name, "--command-os", "*", "registry.svc.ci.openshift.org/%s/release:%s" % (RELEASE_NAMESPACE, name)],
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

        SimpleHTTPServer.SimpleHTTPRequestHandler.do_GET(self)


# Launch multiple listeners as threads
class Thread(threading.Thread):
    def __init__(self, index):
        threading.Thread.__init__(self)
        self.i = index
        self.daemon = True
        self.start()

    def run(self):
        server = FileServer  # SimpleHTTPServer.SimpleHTTPRequestHandler
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
