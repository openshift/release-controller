/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
	v1 "sigs.k8s.io/prow/pkg/apis/prowjobs/v1"
)

// FakeProwJobs implements ProwJobInterface
type FakeProwJobs struct {
	Fake *FakeProwV1
	ns   string
}

var prowjobsResource = v1.SchemeGroupVersion.WithResource("prowjobs")

var prowjobsKind = v1.SchemeGroupVersion.WithKind("ProwJob")

// Get takes name of the prowJob, and returns the corresponding prowJob object, and an error if there is any.
func (c *FakeProwJobs) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.ProwJob, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(prowjobsResource, c.ns, name), &v1.ProwJob{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.ProwJob), err
}

// List takes label and field selectors, and returns the list of ProwJobs that match those selectors.
func (c *FakeProwJobs) List(ctx context.Context, opts metav1.ListOptions) (result *v1.ProwJobList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(prowjobsResource, prowjobsKind, c.ns, opts), &v1.ProwJobList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1.ProwJobList{ListMeta: obj.(*v1.ProwJobList).ListMeta}
	for _, item := range obj.(*v1.ProwJobList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested prowJobs.
func (c *FakeProwJobs) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(prowjobsResource, c.ns, opts))

}

// Create takes the representation of a prowJob and creates it.  Returns the server's representation of the prowJob, and an error, if there is any.
func (c *FakeProwJobs) Create(ctx context.Context, prowJob *v1.ProwJob, opts metav1.CreateOptions) (result *v1.ProwJob, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(prowjobsResource, c.ns, prowJob), &v1.ProwJob{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.ProwJob), err
}

// Update takes the representation of a prowJob and updates it. Returns the server's representation of the prowJob, and an error, if there is any.
func (c *FakeProwJobs) Update(ctx context.Context, prowJob *v1.ProwJob, opts metav1.UpdateOptions) (result *v1.ProwJob, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(prowjobsResource, c.ns, prowJob), &v1.ProwJob{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.ProwJob), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeProwJobs) UpdateStatus(ctx context.Context, prowJob *v1.ProwJob, opts metav1.UpdateOptions) (*v1.ProwJob, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(prowjobsResource, "status", c.ns, prowJob), &v1.ProwJob{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.ProwJob), err
}

// Delete takes name of the prowJob and deletes it. Returns an error if one occurs.
func (c *FakeProwJobs) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(prowjobsResource, c.ns, name, opts), &v1.ProwJob{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeProwJobs) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(prowjobsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1.ProwJobList{})
	return err
}

// Patch applies the patch and returns the patched prowJob.
func (c *FakeProwJobs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.ProwJob, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(prowjobsResource, c.ns, name, pt, data, subresources...), &v1.ProwJob{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.ProwJob), err
}
