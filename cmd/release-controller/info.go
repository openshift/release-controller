package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

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
	client      kubernetes.Interface
	restConfig  *rest.Config
	namespace   string
	name        string
	imageNameFn func() (string, error)
}

func NewExecReleaseInfo(client kubernetes.Interface, restConfig *rest.Config, namespace string, name string, imageNameFn func() (string, error)) *ExecReleaseInfo {
	return &ExecReleaseInfo{
		client:      client,
		restConfig:  restConfig,
		namespace:   namespace,
		name:        name,
		imageNameFn: imageNameFn,
	}
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
		glog.V(4).Infof("Failed to generate changelog:\n$ %s\n%s\n%s", strings.Join(cmd, " "), errOut.String(), out.String())
		return "", fmt.Errorf("could not run remote command: %v", err)
	}

	return out.String(), nil
}

func (r *ExecReleaseInfo) refreshPod() error {
	sts, err := r.client.Apps().StatefulSets(r.namespace).Get("git-cache", metav1.GetOptions{})
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
		if _, err := r.client.Apps().StatefulSets(r.namespace).Create(sts); err != nil {
			return fmt.Errorf("can't create stateful set for cache: %v", err)
		}
		return nil
	}

	sts.Spec = spec
	if _, err := r.client.Apps().StatefulSets(r.namespace).Update(sts); err != nil {
		return fmt.Errorf("can't update stateful set for cache: %v", err)
	}
	return nil
}

func (r *ExecReleaseInfo) specHash(image string) appsv1.StatefulSetSpec {
	spec := appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": r.name,
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
							"storage": resource.MustParse("20Gi"),
						},
					},
				},
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
					{Name: "git-credentials", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "git-credentials"}}},
				},
				Containers: []corev1.Container{
					{
						Name:  "git",
						Image: image,
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: "/tmp"},
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
