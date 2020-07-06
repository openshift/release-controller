package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"google.golang.org/api/iterator"
	"path"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/api/googleapi"

	"cloud.google.com/go/storage"
	"github.com/golang/glog"
	"google.golang.org/api/option"
)

type GCSAuditStore struct {
	bucket     *storage.BucketHandle
	bucketName string
	prefix     string

	lock       sync.Mutex
	signatures map[string][]int
}

func NewGCSAuditStore(bucket string, prefix, userAgent, serviceAccountPath string) (*GCSAuditStore, error) {
	options := []option.ClientOption{
		option.WithScopes(storage.ScopeReadWrite),
	}
	if len(userAgent) > 0 {
		options = append(options, option.WithUserAgent(userAgent))
	}
	if len(serviceAccountPath) > 0 {
		options = append(options, option.WithServiceAccountFile(serviceAccountPath))
	}
	client, err := storage.NewClient(
		context.Background(),
		options...,
	)
	if err != nil {
		return nil, err
	}
	return &GCSAuditStore{
		bucketName: bucket,
		bucket:     client.Bucket(bucket),
		prefix:     strings.Trim(prefix, "/"),

		signatures: make(map[string][]int),
	}, nil
}

func (b *GCSAuditStore) Refresh(ctx context.Context) error {
	signatures := make(map[string][]int)

	q := &storage.Query{Prefix: b.prefix}

	it := b.bucket.Objects(ctx, q)
	for {
		item, err := it.Next()

		if err != nil {
			if err == iterator.Done {
				break
			}
			return err
		}

		path := strings.Trim(strings.TrimPrefix(item.Name, b.prefix), "/")
		parts := strings.SplitN(path, "/", 6)
		switch {
		case parts[0] == "signatures":
			if len(parts) != 5 {
				glog.Warningf("Invalid signature in GCS at path gs://%s/%s", b.bucketName, item.Name)
				continue
			}
			if parts[1] != "openshift" || parts[2] != "release" {
				glog.Warningf("Invalid signature in GCS at path gs://%s/%s", b.bucketName, item.Name)
				continue
			}
			if !strings.HasPrefix(parts[4], "signature-") {
				glog.Warningf("Invalid signature in GCS at path gs://%s/%s", b.bucketName, item.Name)
				continue
			}
			digest := strings.Replace(parts[3], "=", ":", 1)
			valueString := strings.TrimPrefix(parts[4], "signature-")
			index, err := strconv.Atoi(valueString)
			if err != nil {
				glog.Warningf("Invalid signature in GCS at path gs://%s/%s", b.bucketName, item.Name)
				continue
			}
			signatures[digest] = append(signatures[digest], index)
			glog.V(4).Infof("signature %s %d", digest, index)
		default:
			glog.Warningf("Unknown object in GCS at path gs://%s/%s", b.bucketName, item.Name)
		}
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	b.signatures = signatures
	return nil
}

func (b *GCSAuditStore) HasSignature(dgst string) bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	indices := b.signatures[dgst]
	return len(indices) > 0
}

func (b *GCSAuditStore) PutSignature(ctx context.Context, dgst string, signature []byte) error {
	if len(signature) == 0 {
		return fmt.Errorf("invalid signature")
	}
	parts := strings.SplitN(dgst, ":", 2)
	if len(parts) != 2 || (parts[0] != "sha256") || len(parts[1]) != sha256.Size*2 {
		return fmt.Errorf("invalid digest")
	}

	index := b.nextSignatureIndex(dgst)
	var objectPath string
	if len(b.prefix) > 0 {
		objectPath = path.Join(b.prefix, "signatures", "openshift", "release", fmt.Sprintf("%s=%s", parts[0], parts[1]), fmt.Sprintf("signature-%d", index))
	} else {
		objectPath = path.Join("signatures", "openshift", "release", fmt.Sprintf("%s=%s", parts[0], parts[1]), fmt.Sprintf("signature-%d", index))
	}
	glog.V(4).Infof("Writing signature to gs://%s/%s", b.bucketName, objectPath)
	obj := b.bucket.Object(objectPath).If(storage.Conditions{DoesNotExist: true, GenerationMatch: 0})

	w := obj.NewWriter(ctx)
	if _, err := w.Write(signature); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		if gErr, ok := err.(*googleapi.Error); ok && gErr.Code == 412 {
			// TODO: compare the signature contents, if not match, retry and increment index
			return fmt.Errorf("a signature already exists at %s", objectPath)
		}
		return err
	}

	b.lock.Lock()
	defer b.lock.Unlock()
	b.signatures[dgst] = append(b.signatures[dgst], index)
	return nil
}

func (b *GCSAuditStore) nextSignatureIndex(dgst string) int {
	b.lock.Lock()
	defer b.lock.Unlock()
	max := 0
	for _, index := range b.signatures[dgst] {
		if index > max {
			max = index
		}
	}
	return max + 1
}
