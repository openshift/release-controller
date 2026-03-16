#!/usr/bin/env python3
import argparse
import json
import logging

logging.basicConfig(level=logging.INFO, format='[%(asctime)s] %(message)s')
logger = logging.getLogger('convertToQCI')


def generate_reference_tag(status_tags, tag_name, use_sha = False):
    found = False
    generation = 0
    from_name = f'quay-proxy.ci.openshift.org/openshift/ci:ocp_4.23_{tag_name}'

    for status_tag in status_tags:
        if status_tag['tag'] == tag_name:
            found = True
            generation = status_tag['items'][0]['generation']

            if use_sha:
                from_name = f'quay-proxy.ci.openshift.org/openshift/ci@{status_tag["items"][0]["image"]}'

    if found:
        return {
            "annotations": {},
            "from": {
                "kind": "DockerImage",
                "name": from_name
            },
            "generation": generation,
            "importPolicy": {
                "importMode": "PreserveOriginal"
            },
            "name": tag_name,
            "referencePolicy": {
                "type": "Source"
            },
            "reference": True,
        }
    else:
        logger.warning(f'Unable to locate status tag for: {tag_name}')

    return {}

if __name__ == '__main__':
    generate_sha_tags = False

    parser = argparse.ArgumentParser(description='QCI Imagestream Converter')
    parser.add_argument('-s', '--sha', help='Create SHA256 tags', action='store_true')
    args = vars(parser.parse_args())

    if args['sha']:
        generate_sha_tags = True

    with open('cmd/release-controller/testdata/4.23.json') as json_file:
        contents = json.load(json_file)

    specTags = []

    for specTag in contents['spec']['tags']:
        specTags.append(generate_reference_tag(contents['status']['tags'], specTag["name"], generate_sha_tags))

    imagestream = {
        "apiVersion": "image.openshift.io/v1",
        "kind": "ImageStream",
        "metadata": {
            "name": contents['metadata']['name'],
            "namespace": contents['metadata']['namespace'],
        },
        "spec": {
            "lookupPolicy": {
                "local": False
            },
            "tags": specTags
        }
    }

    logger.info(f'\n{json.dumps(imagestream, indent=4, default=str)}\n')