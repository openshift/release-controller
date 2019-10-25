#!/usr/bin/python

from __future__ import print_function

import argparse
import json
import logging
import os
import re
import shutil
import tempfile
import yaml

logging.basicConfig(format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger('generateReleases')
logger.setLevel(logging.INFO)

IMAGE_STREAM_NAME_PATTERN = re.compile(r'^images-ocp-(.+)\.yaml$')
RELEASE_CONFIG_NAME_PATTERN = re.compile(r'^release-ocp-(\d\.\d)(-(.*))?\.json$')
DEFAULT_PROW_JOB_NAME = 'release-controller-test-echo-job'

RELEASE_IMAGES_PATH = 'release/core-services/release-controller'
RELEASE_CONFIGS_PATH = '{0}/_releases'.format(RELEASE_IMAGES_PATH)


class ReleaseGenerator(object):
    def __init__(self):
        self.apply = None
        self.versions = []
        self.output_dir = None
        self.keep_files = None

        self.index = 0
        self.work_dir = tempfile.mkdtemp()

    def _sanitize_release_payload(self, payload):
        data = {}

        for k, v in payload.items():
            if k == 'prowJob':
                data.update({k: {'name': DEFAULT_PROW_JOB_NAME}})
            elif k == 'mirror-to-origin':
                v.update({'disabled': True})
                data.update({k: self._sanitize_release_payload(v)})
            elif isinstance(v, dict):
                data.update({k: self._sanitize_release_payload(v)})
            else:
                data.update({k: v})

        return data

    def _write_image_stream_file(self, content):
        with open('{0}/deploy-{1}.yaml'.format(self.output_dir, self.index), 'w') as release:
            logger.debug('Writing file: {}'.format(release.name))
            yaml.dump(content, release)
            self.index += 1

    def _process_image_streams(self):
        for filename in os.listdir(RELEASE_IMAGES_PATH):
            match = IMAGE_STREAM_NAME_PATTERN.search(filename)

            if match is not None:
                stream_version = match.group(1)

                if len(self.versions) == 0 or stream_version in self.versions:
                    logger.info('Generating image streams for version: {}'.format(stream_version))

                    with open(os.path.join(RELEASE_IMAGES_PATH, filename), 'r') as release_images:
                        image_streams = yaml.safe_load_all(release_images)

                        for image_stream in image_streams:
                            image_stream['metadata']['namespace'] = 'release-controller-test-release'

                            for release_config_filename in os.listdir(RELEASE_CONFIGS_PATH):
                                match = RELEASE_CONFIG_NAME_PATTERN.search(release_config_filename)

                                release_version = None

                                if match is not None:
                                    if match.group(1) is not None:
                                        release_version = match.group(1)
                                    else:
                                        logger.error('Unable to properly parse filename: {}'.format(release_config_filename))
                                        exit(1)

                                if release_version is not None and release_version == stream_version:
                                    with open(os.path.join(RELEASE_CONFIGS_PATH, release_config_filename), 'r') as release_config_file:
                                        release_config = self._sanitize_release_payload(json.load(release_config_file))

                                        if image_stream['metadata']['name'] == release_config['mirrorPrefix']:
                                            image_stream['metadata']['annotations'] = {
                                                'release.openshift.io/config': json.dumps(release_config)
                                            }

                                            self._write_image_stream_file(image_stream)
                                            break

    def _generate_release_stream(self):
        logger.info('Generating "release" image stream: 4.y-stable')
        with open(os.path.join(RELEASE_CONFIGS_PATH, 'release-ocp-4.y-stable.json'), 'r') as release_config_file:
            release_config = self._sanitize_release_payload(json.load(release_config_file))

            release_stream = {
                'kind': 'ImageStream',
                'apiVersion': 'v1',
                'metadata': {
                    'namespace': 'release-controller-test-release',
                    'name': 'release',
                    'annotations': {
                        'release.openshift.io/config': json.dumps(release_config)
                    }
                }
            }

            self._write_image_stream_file(release_stream)

    def run(self):
        os.chdir(self.work_dir)
        os.system('git clone https://github.com/openshift/release.git')

        self._process_image_streams()
        self._generate_release_stream()

        if self.apply:
            logger.info('Applying content from: {}'.format(self.output_dir))
            os.system('oc apply -R -f {}'.format(self.output_dir))

        if not self.keep_files:
            logger.debug('Deleting working directory: {}'.format(self.work_dir))
            shutil.rmtree(self.work_dir, ignore_errors=True)

    def parse_args(self):
        parser = argparse.ArgumentParser(description='Generates OpenShift Release ImageStreams.')
        parser.add_argument('-a', '--apply', help='Apply generated content.', action='store_true')
        parser.add_argument('-k', '--keep', help='Keep working directory.', action='store_true')
        parser.add_argument('-o', '--output', help='Full path to the directory to store generated content (default is inside working directory).', default=None)
        parser.add_argument('-v', '--version', help='OpenShift Release(s) to generate. (i.e. 4.3)', action='append', default=None)
        parser.add_argument('-d', '--debug', help='Enable debug output.', action='store_true')

        opts = parser.parse_args()

        if opts.debug:
            logger.setLevel(logging.DEBUG)

        logger.debug('Working directory: {}'.format(self.work_dir))

        self.apply = opts.apply
        self.keep_files = opts.keep

        if opts.version is not None:
            self.versions = opts.version

        if opts.output is None:
            self.output_dir = tempfile.mkdtemp(dir=self.work_dir)
        else:
            if not opts.output.startswith('/') or not os.path.exists(opts.output):
                logger.error('Output folder is not a full path or does not exist: {}'.format(opts.output))
                exit(1)
            else:
                self.output_dir = opts.output
                logger.info('Writing output to directory: {}'.format(self.output_dir))


if __name__ == '__main__':
    tool = ReleaseGenerator()
    tool.parse_args()
    tool.run()
