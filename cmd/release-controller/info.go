package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/golang/groupcache"

	"github.com/golang/glog"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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
		parts := strings.Split(key, "\x00")
		s, err := info.ChangeLog(parts[0], parts[1])
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
	err := c.cache.Get(context.TODO(), strings.Join([]string{from, to}, "\x00"), groupcache.StringSink(&s))
	return s, err
}

type ReleaseInfo interface {
	ChangeLog(from, to string) (string, error)
}

type ExecReleaseInfo struct {
	client     kubernetes.Interface
	restConfig *rest.Config
	namespace  string
	name       string
	image      string
}

func NewExecReleaseInfo(client kubernetes.Interface, restConfig *rest.Config, namespace string, name string) *ExecReleaseInfo {
	return &ExecReleaseInfo{
		client:     client,
		restConfig: restConfig,
		namespace:  namespace,
		name:       name,
		image:      "registry.svc.ci.openshift.org/ocp/4.0:tests",
	}
}

func (r *ExecReleaseInfo) specHash() (appsv1.ReplicaSetSpec, string) {
	spec := appsv1.ReplicaSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": r.name,
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app": r.name,
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "git", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				},
				Containers: []corev1.Container{
					{
						Name:  "git",
						Image: r.image,
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: "/tmp"},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "git", MountPath: "/tmp/git/"},
						},
						Command: []string{
							"/bin/bash",
							"-c",
							`#!/bin/bash
							set -euo pipefail
							trap 'kill $(jobs -p); exit 0' TERM

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
	data, _ := json.Marshal(spec)
	hash := fnv.New128()
	hash.Write(data)
	return spec, base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
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

	rs, err := r.client.Apps().ReplicaSets(r.namespace).Get(r.name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return "", err
	}

	// increment this when you change the replica set definition
	spec, hash := r.specHash()

	if rs != nil && rs.Annotations["release-hash"] != hash {
		oldHash := rs.Annotations["release-hash"]
		if glog.V(4) {
			glog.Infof("replica set changed: %s to %s", rs.Annotations["release-hash"], hash)
		}
		if rs.DeletionTimestamp != nil {
			glog.V(4).Infof("wait for replica set to be deleted")
			wait.Poll(time.Second, time.Minute, func() (bool, error) {
				_, err := r.client.Apps().ReplicaSets(r.namespace).Get(r.name, metav1.GetOptions{})
				return errors.IsNotFound(err), nil
			})
		} else {
			foreground := metav1.DeletePropagationForeground
			if err := r.client.Apps().ReplicaSets(r.namespace).Delete(r.name, &metav1.DeleteOptions{PropagationPolicy: &foreground}); err != nil {
				if !errors.IsNotFound(err) {
					return "", err
				}
			}
			if err := wait.PollImmediate(3*time.Second, time.Minute, func() (bool, error) {
				rs, err := r.client.Apps().ReplicaSets(r.namespace).Get(r.name, metav1.GetOptions{})
				if err != nil {
					if !errors.IsNotFound(err) {
						return false, err
					}
					return true, nil
				}
				if rs.Annotations["release-hash"] == hash {
					return true, nil
				}
				if rs.Annotations["release-hash"] == oldHash {
					return false, nil
				}
				return false, fmt.Errorf("another server was set up while we were waiting for deletion: %s", rs.Annotations["release-hash"])
			}); err != nil {
				return "", err
			}
			glog.V(4).Infof("foreground deletion completed")
		}
		rs = nil
	}
	if rs == nil {
		_, err = r.client.Apps().ReplicaSets(r.namespace).Create(&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: r.name,
				Annotations: map[string]string{
					"release-hash": hash,
				},
			},
			Spec: spec,
		})
		if err != nil {
			return "", err
		}
	}

	podClient := r.client.Core()

	var podName string
	if err := wait.PollImmediate(3*time.Second, time.Minute, func() (bool, error) {
		pods, err := podClient.Pods(r.namespace).List(metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", r.name)})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.ObjectMeta.DeletionTimestamp != nil {
				continue
			}
			if pod.Status.Phase == corev1.PodRunning {
				podName = pod.Name
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return "", err
	}

	cmd := []string{"oc", "adm", "release", "info", "--changelog=/tmp/git/", from, to}
	u := podClient.RESTClient().Post().Resource("pods").Namespace(r.namespace).Name(podName).SubResource("exec").VersionedParams(&corev1.PodExecOptions{
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
		glog.V(4).Infof("Failed to generate changelog:\n$ %s\n%s\n%s", strings.Join(cmd, " "), errOut.String(), out.String())
		return "", fmt.Errorf("could not run remote command: %v", err)
	}

	return out.String(), nil
}
