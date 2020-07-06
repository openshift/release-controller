package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"k8s.io/klog"
)

type FileAuditStore struct {
	path string

	lock       sync.Mutex
	signatures map[string][]int
}

func NewFileAuditStore(path string) (*FileAuditStore, error) {
	return &FileAuditStore{
		path: path,

		signatures: make(map[string][]int),
	}, nil
}

func (b *FileAuditStore) Refresh(ctx context.Context) error {
	signatures := make(map[string][]int)

	err := filepath.Walk(b.path, func(item string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		path := strings.TrimPrefix(item, b.path)
		parts := strings.SplitN(path, "/", 6)
		switch {
		case parts[0] == "signatures":
			if len(parts) != 5 {
				klog.Warningf("Invalid signature at path %s", item)
				return nil
			}
			if parts[1] != "openshift" || parts[2] != "release" {
				klog.Warningf("Invalid signature at path %s", item)
				return nil
			}
			if !strings.HasPrefix(parts[4], "signature-") {
				klog.Warningf("Invalid signature at path %s", item)
				return nil
			}
			digest := strings.Replace(parts[3], "=", ":", 1)
			valueString := strings.TrimPrefix(parts[4], "signature-")
			index, err := strconv.Atoi(valueString)
			if err != nil {
				klog.Warningf("Invalid signature at path %s", item)
				return nil
			}
			signatures[digest] = append(signatures[digest], index)
			klog.V(4).Infof("signature %s %d", digest, index)
		default:
			klog.Warningf("Invalid signature at path %s", item)
		}
		return nil
	})
	if err != nil {
		return err
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	b.signatures = signatures
	return nil
}

func (b *FileAuditStore) HasSignature(dgst string) bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	indices := b.signatures[dgst]
	return len(indices) > 0
}

func (b *FileAuditStore) PutSignature(ctx context.Context, dgst string, signature []byte) error {
	if len(signature) == 0 {
		return fmt.Errorf("invalid signature")
	}
	parts := strings.SplitN(dgst, ":", 2)
	if len(parts) != 2 || (parts[0] != "sha256") || len(parts[1]) != sha256.Size*2 {
		return fmt.Errorf("invalid digest")
	}

	index := b.nextSignatureIndex(dgst)
	dir := filepath.Join(b.path, "signatures", "openshift", "release", fmt.Sprintf("%s=%s", parts[0], parts[1]))
	path := filepath.Join(dir, fmt.Sprintf("signature-%d", index))

	if err := os.MkdirAll(dir, 0777); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(signature); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	b.lock.Lock()
	defer b.lock.Unlock()
	b.signatures[dgst] = append(b.signatures[dgst], index)
	return nil
}

func (b *FileAuditStore) nextSignatureIndex(dgst string) int {
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
