package signer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp"
	"k8s.io/klog"
)

// Interface performs signing and verification of the provided content. The default implementation
// in this package uses the container signature format defined at https://github.com/containers/image
// to authenticate that a given release image digest has been signed by a trusted party.
type Interface interface {
	Verify(ctx context.Context, releaseDigest, location string, signature []byte) error
	Sign(releaseDigest, pullSpec string) ([]byte, error)
}

func loadArmoredOrUnarmoredGPGKeyRing(data []byte) (openpgp.EntityList, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	if err == nil {
		return keyring, nil
	}
	return openpgp.ReadKeyRing(bytes.NewReader(data))
}

type releaseSigner struct {
	signer    *openpgp.Entity
	verifiers map[string]openpgp.EntityList
}

func NewFromKeyring(path string) (Interface, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keyring, err := loadArmoredOrUnarmoredGPGKeyRing(data)
	if err != nil {
		return nil, err
	}
	var signer *openpgp.Entity
	for _, key := range keyring {
		if key.PrimaryKey.CanSign() && key.PrivateKey != nil {
			signer = key
			break
		}
	}
	if signer == nil {
		return nil, fmt.Errorf("the provided keyring must contain a private key capable of signing")
	}
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	return &releaseSigner{
		signer: signer,
		verifiers: map[string]openpgp.EntityList{
			name: keyring,
		},
	}, nil
}

// String summarizes the verifier for human consumption
func (s *releaseSigner) String() string {
	var keys []string
	for name := range s.verifiers {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.Grow(256)
	fmt.Fprintf(&builder, "All release image digests must have GPG signatures from")
	if len(keys) == 0 {
		fmt.Fprint(&builder, " <ERROR: no verifiers>")
	}
	for _, name := range keys {
		verifier := s.verifiers[name]
		fmt.Fprintf(&builder, " %s (", name)
		for i, entity := range verifier {
			if i != 0 {
				fmt.Fprint(&builder, ", ")
			}
			if entity.PrimaryKey != nil {
				fmt.Fprintf(&builder, strings.ToUpper(fmt.Sprintf("%x", entity.PrimaryKey.Fingerprint)))
				fmt.Fprint(&builder, ": ")
			}
			count := 0
			for identityName := range entity.Identities {
				if count != 0 {
					fmt.Fprint(&builder, ", ")
				}
				fmt.Fprintf(&builder, "%s", identityName)
				count++
			}
		}
		fmt.Fprint(&builder, ")")
	}
	return builder.String()
}

func (s *releaseSigner) Sign(releaseDigest, pullSpec string) ([]byte, error) {
	if len(releaseDigest) == 0 || len(pullSpec) == 0 {
		return nil, fmt.Errorf("you must specify a release digest and pull spec to sign")
	}

	sig := &signature{
		Critical: criticalSignature{
			Type: "atomic container signature",
			Image: criticalImage{
				DockerManifestDigest: releaseDigest,
			},
			Identity: criticalIdentity{
				DockerReference: pullSpec,
			},
		},
		Optional: optionalSignature{
			Creator:   "openshift release-controller",
			Timestamp: time.Now().Unix(),
		},
	}
	message, err := json.MarshalIndent(sig, "", "  ")
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	w, err := openpgp.Sign(buf, s.signer, nil, nil)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(message); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var ErrSignatureNotValid = fmt.Errorf("the provided signature could not be verified against any key")

func (s *releaseSigner) Verify(ctx context.Context, releaseDigest, location string, signature []byte) error {
	if len(s.verifiers) == 0 {
		return fmt.Errorf("the release verifier is incorrectly configured, unable to verify digests")
	}
	if len(releaseDigest) == 0 {
		return fmt.Errorf("release images that are not accessed via digest cannot be verified")
	}

	for k, keyring := range s.verifiers {
		content, _, err := verifySignatureWithKeyring(bytes.NewReader(signature), keyring)
		if err != nil {
			klog.V(4).Infof("keyring %q could not verify signature: %s", k, err)
			continue
		}
		if err := verifyAtomicContainerSignature(content, releaseDigest); err != nil {
			klog.V(4).Infof("signature %q is not valid: %s", location, err)
			continue
		}
		return nil
	}
	return ErrSignatureNotValid
}

// verifySignatureWithKeyring performs a containers/image verification of the provided signature
// message, checking for the integrity and authenticity of the provided message in r. It will return
// the identity of the signer if successful along with the message contents.
func verifySignatureWithKeyring(r io.Reader, keyring openpgp.EntityList) ([]byte, string, error) {
	md, err := openpgp.ReadMessage(r, keyring, nil, nil)
	if err != nil {
		return nil, "", fmt.Errorf("could not read the message: %s", err)
	}
	if !md.IsSigned {
		return nil, "", fmt.Errorf("not signed")
	}
	content, err := ioutil.ReadAll(md.UnverifiedBody)
	if err != nil {
		return nil, "", err
	}
	if md.SignatureError != nil {
		return nil, "", fmt.Errorf("signature error: %s", md.SignatureError)
	}
	if md.SignedBy == nil {
		return nil, "", fmt.Errorf("invalid signature")
	}
	if md.Signature != nil {
		if md.Signature.SigLifetimeSecs != nil {
			expiry := md.Signature.CreationTime.Add(time.Duration(*md.Signature.SigLifetimeSecs) * time.Second)
			if time.Now().After(expiry) {
				return nil, "", fmt.Errorf("signature expired on %s", expiry)
			}
		}
	} else if md.SignatureV3 == nil {
		return nil, "", fmt.Errorf("unexpected openpgp.MessageDetails: neither Signature nor SignatureV3 is set")
	}

	// follow conventions in containers/image
	return content, strings.ToUpper(fmt.Sprintf("%x", md.SignedBy.PublicKey.Fingerprint)), nil
}

// An atomic container signature has the following schema:
//
// {
// 	"critical": {
// 			"type": "atomic container signature",
// 			"image": {
// 					"docker-manifest-digest": "sha256:817a12c32a39bbe394944ba49de563e085f1d3c5266eb8e9723256bc4448680e"
// 			},
// 			"identity": {
// 					"docker-reference": "docker.io/library/busybox:latest"
// 			}
// 	},
// 	"optional": {
// 			"creator": "some software package v1.0.1-35",
// 			"timestamp": 1483228800,
// 	}
// }
type signature struct {
	Critical criticalSignature `json:"critical"`
	Optional optionalSignature `json:"optional"`
}

type criticalSignature struct {
	Type     string           `json:"type"`
	Image    criticalImage    `json:"image"`
	Identity criticalIdentity `json:"identity"`
}

type criticalImage struct {
	DockerManifestDigest string `json:"docker-manifest-digest"`
}

type criticalIdentity struct {
	DockerReference string `json:"docker-reference"`
}

type optionalSignature struct {
	Creator   string `json:"creator"`
	Timestamp int64  `json:"timestamp"`
}

// verifyAtomicContainerSignature verifiers that the provided data authenticates the
// specified release digest. If error is returned the provided data does NOT authenticate
// the release digest and the signature must be ignored.
func verifyAtomicContainerSignature(data []byte, releaseDigest string) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var sig signature
	if err := d.Decode(&sig); err != nil {
		return fmt.Errorf("the signature is not valid JSON: %s", err)
	}
	if sig.Critical.Type != "atomic container signature" {
		return fmt.Errorf("signature is not the correct type")
	}
	if len(sig.Critical.Identity.DockerReference) == 0 {
		return fmt.Errorf("signature must have an identity")
	}
	if sig.Critical.Image.DockerManifestDigest != releaseDigest {
		return fmt.Errorf("signature digest does not match")
	}
	return nil
}
