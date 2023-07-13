# release-tool.py
Provides `OpenShift Release Admins` a singular tool to perform operations on the pieces and parts responsible for producing an OpenShift Release as well as manually accepting and rejecting the releases themselves. 

### Usage

```bash
$ ./hack/release-tool.py --help
usage: release-tool.py [-h] [-m MESSAGE] [-r REASON] [-o OUTPUT] [--execute] [-v] [-c CONTEXT] [-k KUBECONFIG] [-n {ocp,okd}] [-i IMAGESTREAM] [-a {amd64,arm64,ppc64le,s390x,multi}] [-p] {accept,reject,prune,archive,import} ...

Manually accept or reject release payloads

options:
  -h, --help            show this help message and exit
  -m MESSAGE, --message MESSAGE
                        Specifies a custom message to include with the update
  -r REASON, --reason REASON
                        Specifies a custom reason to include with the update
  -o OUTPUT, --output OUTPUT
                        The location where backup files will be stored. If not specified, a temporary location will be used.
  --execute             Specify to persist changes on the cluster

Configuration Options:
  -v, --verbose         Enable verbose output

Openshift Configuration Options:
  -c CONTEXT, --context CONTEXT
                        The OC context to use (default is "app.ci")
  -k KUBECONFIG, --kubeconfig KUBECONFIG
                        The kubeconfig to use (default is "~/.kube/config")
  -n {ocp,okd}, --name {ocp,okd}
                        The product prefix to use (default is "ocp")
  -i IMAGESTREAM, --imagestream IMAGESTREAM
                        The name of the release imagestream to use (default is "release")
  -a {amd64,arm64,ppc64le,s390x,multi}, --architecture {amd64,arm64,ppc64le,s390x,multi}
                        The architecture of the release to process (default is "amd64")
  -p, --private         Enable updates of "private" releases

subcommands:
  valid subcommands

  {accept,reject,prune,archive,import}
                        Supported operations
    accept              Accepts the specified release
    reject              Rejects the specified release
    prune               Prunes the specified release(s)
    archive             Archives tags from the specified imagestream
    import              Manually import all tags defined in the respective imagestream
```

### Accept a release:

```bash
$ ./hack/release-tool.py --execute accept <RELEASE>
```

#### Examples

##### amd64

```bash
$ ./hack/release-tool.py --execute accept 4.13.0-0.nightly-2023-02-01-192642
```

##### s390x

```bash
$ ./hack/release-tool.py --execute -a s390x accept 4.13.0-0.nightly-s390x-2023-02-01-192642
```

##### ppc64le

```bash
$ ./hack/release-tool.py --execute -a ppc64le accept 4.13.0-0.nightly-ppc64le-2023-02-01-192642 
```

##### arm64

```bash
$ ./hack/release-tool.py --execute -a arm64 accept 4.13.0-0.nightly-arm64-2023-02-01-192642
```

##### multi

```bash
$ ./hack/release-tool.py --execute -a multi -i 4.13-art-latest accept 4.13.0-0.nightly-multi-2023-02-01-192642
```

### Reject a release

```bash
$ ./hack/release-tool.py --execute reject <RELEASE>
```

#### Examples

##### amd64

```bash
$ ./hack/release-tool.py --execute reject 4.13.0-0.nightly-2023-02-01-192642
```

##### s390x

```bash
$ ./hack/release-tool.py --execute -a s390x reject 4.13.0-0.nightly-s390x-2023-02-01-192642
```

##### ppc64le

```bash
$ ./hack/release-tool.py --execute -a ppc64le reject 4.13.0-0.nightly-ppc64le-2023-02-01-192642 
```

##### arm64

```bash
$ ./hack/release-tool.py --execute -a arm64 reject 4.13.0-0.nightly-arm64-2023-02-01-192642
```

##### multi

```bash
$ ./hack/release-tool.py --execute -a multi -i 4.13-art-latest reject 4.13.0-0.nightly-multi-2023-02-01-192642
```

### Pruning release(s)

```bash
$ ./hack/release-tool.py --execute prune -y <Space seperated list of RELEASES>
```

#### Example

```bash
$ ./hack/release-tool.py --execute prune -y 4.14.0-0.ci-2023-06-23-200722 4.14.0-0.ci-2023-06-24-020722 4.14.0-0.ci-2023-06-24-080722 4.14.0-0.nightly-2023-06-23-234912 4.14.0-0.nightly-2023-06-24-081005 4.14.0-0.nightly-2023-06-24-145624 4.13.0-0.ci-2023-06-23-063956 4.13.0-0.ci-2023-06-23-124953 4.13.0-0.ci-2023-06-23-184953 4.13.0-0.nightly-2023-06-22-192224
```

### Archiving EOL Releases

```bash
$ ./hack/release-tool.py --execute archive -y <Space seperated list of version prefixes (i.e X.Y)>
```

#### Example

```bash
$ ./hack/release-tool.py --execute archive -y 4.6
```

### Re-Import tags for releases stuck in "Pending" state

```bash
$ ./hack/release-tool.py --execute import <IMAGESTREAM>
```

#### Examples

##### amd64

```bash
$ ./hack/release-tool.py --execute import 4.13-art-latest
```

##### s390x

```bash
$ ./hack/release-tool.py -a s390x --execute import 4.13-art-latest
```

##### ppc64le

```bash
$ ./hack/release-tool.py -a ppc64le --execute import 4.14-art-latest
```

##### arm64

```bash
$ ./hack/release-tool.py -a arm64 --execute import 4.9-art-latest
```
