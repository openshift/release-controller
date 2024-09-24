#!/usr/bin/env python3

import argparse
import json
import logging
import os.path
import re
import tempfile
import time
import typing

import openshift_client as oc
from openshift_client import OpenShiftPythonException, Missing

logging.basicConfig(level=logging.INFO, format='[%(asctime)s] %(message)s')
logger = logging.getLogger('releaseTool')

SUPPORTED_PRODUCTS = ['ocp', 'okd']
SUPPORTED_ARCHITECTURES = ['amd64', 'arm64', 'ppc64le', 's390x', 'multi']

ARCHIVE_VERSION_PREFIX = re.compile(r'^(\d+)\.(\d+)$')


def generate_resource_values(product, stream_name, architecture, private):
    arch_suffix, private_suffix = "", ""

    if product == 'okd':
        return 'origin', stream_name

    if architecture != 'amd64':
        arch_suffix = f'-{architecture}'

    if private:
        private_suffix = '-priv'

    namespace = f'{product}{arch_suffix}{private_suffix}'
    imagestream = f'{stream_name}{arch_suffix}{private_suffix}'

    return namespace, imagestream


def validate_server_connection(ctx):
    with oc.options(ctx), oc.tracking(), oc.timeout(60):
        try:
            username = oc.whoami()
            version = oc.get_server_version()
            logger.debug(f'Connected to APIServer running version: {version}, as: {username}')
        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f"Unable to verify cluster connection using context: \"{ctx['context']}\"")
            raise e


def create_imagestreamtag_patch(action, custom_message, custom_reason):
    data = {
        'image': {
            'metadata': {
                'annotations': {}
            }
        },
        'metadata': {
            'annotations': {}
        },
        'tag': {
            'annotations': {}
        }
    }

    if action == 'accept':
        phase = 'Accepted'
    elif action == 'reject':
        phase = 'Rejected'
    else:
        raise ValueError(f'Unsupported action specified: {action}')

    message = f'Manually {action}ed per TRT'
    if custom_message is not None:
        message = custom_message

    annotations = {
        'phase': phase,
        'message': message
    }

    if custom_reason is not None:
        annotations['reason'] = custom_reason

    for key, value in annotations.items():
        if value is not None:
            annotation = 'release.openshift.io/' + key
            data['image']['metadata']['annotations'][annotation] = value
            data['metadata']['annotations'][annotation] = value
            data['tag']['annotations'][annotation] = value

    return data


def write_backup_file(path, name, release, data):
    ts = int(round(time.time() * 1000))
    backup_filename = f'{path}/{name}_{release}-{ts}.json'

    with open(backup_filename, mode='w+', encoding='utf-8') as backup:
        logger.debug(f'Creating backup file: {backup_filename}')
        backup.write(json.dumps(data, indent=4))

    return backup_filename


def patch_imagestreamtag(ctx, namespace, imagestream, action, release, custom_message, custom_reason, execute, output_path):
    patch = create_imagestreamtag_patch(action, custom_message, custom_reason)
    logger.debug(f'Generated oc patch:\n{json.dumps(patch, indent=4)}')

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                tag = oc.selector(f'imagestreamtag/{imagestream}:{release}').object(ignore_not_found=True)
                if not tag:
                    logger.error(f'Unable to locate imagestreamtag: {namespace}/{imagestream}:{release}')
                    return

                logger.info(f'{action.capitalize()}ing imagestreamtag: {namespace}/{imagestream}:{release}')
                if execute:
                    backup_file = write_backup_file(output_path, imagestream, release, tag.model._primitive())

                    tag.patch(patch)

                    logger.info(f'Release {release} updated successfully')
                    logger.info(f'Backup written to: {backup_file}')
                else:
                    logger.info(f'[dry-run] Patching release {release} with patch:\n{json.dumps(patch, indent=4)}')
                    logger.warning('You must specify "--execute" to permanently apply these changes')

        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to update release: "{release}"')
            raise e


def create_releasepayload_patch(action, custom_reason):
    if action == 'accept':
        override = 'Accepted'
    elif action == 'reject':
        override = 'Rejected'
    else:
        raise ValueError(f'Unsupported action specified: {action}')

    reason = f'Manually {action}ed per TRT'
    if custom_reason is not None:
        reason = custom_reason

    data = {
        'spec': {
            'payloadOverride': {
                'override': override,
                'reason': reason,
            }
        }
    }

    return data


def patch_releaespayload(ctx, namespace, action, release, custom_reason, execute, output_path):
    patch = create_releasepayload_patch(action, custom_reason)
    logger.debug(f'Generated oc patch:\n{json.dumps(patch, indent=4)}')

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                payload = oc.selector(f'releasepayload/{release}').object(ignore_not_found=True)
                if not payload:
                    logger.error(f'Unable to locate releasepayload: {namespace}/{release}')
                    return

                logger.info(f'{action.capitalize()}ing releasepayload: {namespace}/{release}')
                if execute:
                    backup_file = write_backup_file(output_path, "releasepayload", release, payload.model._primitive())

                    payload.patch(patch, strategy='merge')

                    logger.info(f'ReleasePayload {release} updated successfully')
                    logger.info(f'Backup written to: {backup_file}')
                else:
                    logger.info(f'[dry-run] Patching releasepayload {release} with patch:\n{json.dumps(patch, indent=4)}')
                    logger.warning('You must specify "--execute" to permanently apply these changes')

        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to update releasepayload: "{release}"')
            raise e


def prune_release_tags(ctx, namespace, imagestream, releases, execute, confirm, output_path):
    for tag in releases:
        if execute:
            delete_imagestreamtag(ctx, namespace, imagestream, tag, confirm, output_path)
        else:
            logger.info(f'[dry-run] Deleting imagestreamtag: {namespace}/{imagestream}:{tag}')
            logger.warning('You must specify "--execute" to permanently apply these changes')


def delete_imagestreamtag(ctx, namespace, imagestream, tag, confirm, output_path):
    imagestreamtag = f'{imagestream}:{tag}'

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                result = oc.selector(f'imagestreamtag/{imagestreamtag}').object(ignore_not_found=True)

                if result is not None:
                    # Check for the release-controllers "keep" annotation...
                    keep = result.get_annotation("release.openshift.io/keep", if_missing=None)

                    if keep is None and (confirm or confirm_delete(namespace, imagestreamtag)):
                        logger.info(f'Deleting imagestreamtag: {namespace}/{imagestreamtag}')

                        backup_file = write_backup_file(output_path, "imagestreamtag", tag, result.model._primitive())
                        logger.info(f'Backup written to: {backup_file}')

                        r = result.delete(ignore_not_found=True)
                        if r.status() != 0:
                            logger.error(f'Delete returned: {r.out()}')
                    else:
                        if keep is not None:
                            logger.warning(f'Imagestreamtag: "{namespace}/{imagestreamtag}" has been flagged as "Keep", skipped.')
                        else:
                            logger.info(f'Deletion of imagestreamtag: "{namespace}/{imagestreamtag}" skipped.')
                else:
                    logger.info(f'Imagestreamtag: "{namespace}/{imagestreamtag}" does not exist.')
        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to delete imagestreamtag: {e}')
            raise e


def confirm_delete(namespace, imagestreamtag):
    i = 1
    while i <= 5:
        answer = input(f"Delete: {namespace}/{imagestreamtag}? (yes or no) ")
        if any(answer.lower() == f for f in ["yes", 'y', 'ye']):
            return True
        elif any(answer.lower() == f for f in ['no', 'n', '0']):
            return False
        else:
            print('Please enter yes or no')
            i = i + 1
    return False


def validate_prefixes(prefixes):
    invalid_prefixes = []
    for prefix in prefixes:
        match = ARCHIVE_VERSION_PREFIX.search(prefix)

        if match is None:
            invalid_prefixes.append(prefix)

    if len(invalid_prefixes) > 0:
        logger.error(f'Invalid prefix(es) specified: {",".join(invalid_prefixes)}')
        exit(len(invalid_prefixes))


def archive(ctx, namespace, imagestream, prefixes, execute, confirm, output_path):
    logger.info(f'Archiving {",".join(prefixes)} tags from imagestream: {namespace}/{imagestream}')

    items_to_archive = process_imagestream(ctx, namespace, imagestream, prefixes)
    logger.debug(f'Items to archive:\n{json.dumps(items_to_archive, indent=4, default=str)}')

    for item in items_to_archive:
        for key in item.keys():
            archive_name = f'{imagestream}-archive-{key}'
            archive_imagestream, spec_tags, status_tags = build_archive_imagestream(namespace, archive_name, item[key])

            proceed = create_archive_imagestream(ctx, namespace, archive_name, archive_imagestream, execute)
            if proceed:
                proceed = patch_archive_imagestream(ctx, namespace, archive_name, status_tags, execute, output_path)

            if proceed:
                delete_archived_imagestreamtags(ctx, namespace, imagestream, spec_tags, status_tags, execute, confirm, output_path)


def process_imagestream(ctx, namespace, imagestream, prefixes):
    items = []

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                result = oc.selector(f'imagestream/{imagestream}').object(ignore_not_found=True)

                if result is not None:
                    for tag in result.model.spec.tags:
                        for prefix in prefixes:
                            tags_to_archive = get_tags_to_archive(items, prefix)

                            if tag.name.startswith(f'{prefix}.'):
                                data = {
                                    'name': tag.name,
                                    'annotations': tag.annotations
                                }
                                if tag['from'] is not Missing and tag['from'].kind == "DockerImage":
                                    data['from'] = tag['from']
                                    tags_to_archive.append(data)
                                else:
                                    for status_tag in result.model.status.tags:
                                        if tag.name == status_tag.tag:
                                            data['status_tag'] = status_tag
                                            tags_to_archive.append(data)
                else:
                    logger.info(f'Imagestream: "{namespace}/{imagestream}" does not exist.')
        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to process imagestream: {e}')
            raise e

    return items


def get_tags_to_archive(items, prefix):
    for item in items:
        if prefix in item:
            return item[prefix]

    item = {prefix: []}
    items.append(item)

    return item[prefix]


def build_archive_imagestream(namespace, name, tags_to_archive):
    spec_tags = []
    status_tags = []

    for tag in tags_to_archive:
        data = {
            'annotations': tag['annotations'],
            'name': tag['name'],
            'importPolicy': {},
            'referencePolicy': {
                'type': 'Source'
            }
        }
        if 'from' in tag:
            data['from'] = tag['from']
        elif 'status_tag' in tag:
            status_tags.append(tag['status_tag'])

        spec_tags.append(data)

    imagestream = {
        'apiVersion': 'image.openshift.io/v1',
        'kind': 'ImageStream',
        'metadata': {
            'name': name,
            'namespace': namespace,
        },
        'spec': {
            'lookupPolicy': {
                'local': False
            },
            'tags': spec_tags
        }
    }
    return imagestream, spec_tags, status_tags


def create_archive_imagestream(ctx, namespace, name, payload, execute):
    logger.debug(f'Creating archive imagestream {namespace}/{name}:\n{json.dumps(payload, indent=4, default=str)}')

    with oc.options(ctx), oc.tracking(), oc.timeout(30 * 60):
        try:
            with oc.project(namespace):
                imagestream = oc.selector(f'imagestream/{name}').object(ignore_not_found=True)
                if imagestream:
                    logger.error(f'Archive imagestream: {namespace}/{name} already exists')
                    return False

                if execute:
                    sel = oc.create(payload)
                    sel.until_all(1, success_func=oc.status.is_imagestream_imported)
                    logger.info(f'Archive imagestream {namespace}/{name} created successfully')
                    return True
                else:
                    logger.info(f'[dry-run] Creating archive imagestream: {namespace}/{name}')
                    logger.warning('You must specify "--execute" to permanently apply these changes')
        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to create archive imagestream: "{namespace}/{name}"')
            raise e

    return False


def patch_archive_imagestream(ctx, namespace, name, status_tags, execute, output_path):
    patch = create_imagestream_status_patch(status_tags)
    logger.debug(f'Generated oc patch:\n{json.dumps(patch, indent=4)}')

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                imagestream = oc.selector(f'imagestream/{name}').object(ignore_not_found=True)
                if not imagestream:
                    logger.error(f'Unable to locate imagestream: {namespace}/{name}')
                    return False

                logger.info(f'Patching archive imagestream: {namespace}/{name}')
                if execute:
                    backup_file = write_backup_file(output_path, 'imagestream', name, imagestream.model._primitive())

                    imagestream.patch(patch, cmd_args='--subresource=status')

                    logger.info(f'Archive imagestream {name} updated successfully')
                    logger.info(f'Backup written to: {backup_file}')
                    return True
                else:
                    logger.info(f'[dry-run] Patching archive imagestream {name} with patch:\n{json.dumps(patch, indent=4)}')
                    logger.warning('You must specify "--execute" to permanently apply these changes')
        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to update archive imagestream: "{name}"')
            raise e

    return False


def create_imagestream_status_patch(status_tags):
    data = {
        'status': {
            'tags': []
        }
    }

    for tag in status_tags:
        data['status']['tags'].append(tag)

    return data


def delete_archived_imagestreamtags(ctx, namespace, name, spec_tags, status_tags, execute, confirm, output_path):
    tags = []
    for spec in spec_tags:
        if spec['name'] not in tags:
            tags.append(spec['name'])
    for status in status_tags:
        if status.tag not in tags:
            tags.append(status.tag)

    prune_release_tags(ctx, namespace, name, tags, execute, confirm, output_path)


def reimport(ctx, namespace, imagestream, execute):
    logger.info(f'Importing tags from imagestream: {namespace}/{imagestream}')

    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.project(namespace):
                stream = oc.selector(f'imagestream/{imagestream}').object(ignore_not_found=True)
                if not stream:
                    logger.error(f'Unable to locate imagestream: {namespace}/{imagestream}')
                    return

                logger.debug(f'Imagestream:\n{json.dumps(stream.model, indent=4, default=str)}')

                for tag in stream.model.status.tags:
                    if tag.conditions != Missing:
                        for condition in tag.conditions:
                            if condition.type == 'ImportSuccess' and condition.status == 'False':
                                if execute:
                                    logger.info(f'Importing: {imagestream}:{tag.tag}')
                                    oc.invoke('import-image', cmd_args=[f'is/{imagestream}:{tag.tag}'])
                                else:
                                    logger.info(f'[dry-run] Importing: {imagestream}:{tag.tag}')

        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to process imagestream: "{namespace}/{imagestream}"')
            raise e


class NightlyComponents(typing.NamedTuple):
    major_minor: str
    arch: str
    is_private: bool
    timestamp: str

    def arch_suffix(self):
        if self.arch != 'amd64':
            return f'-{self.arch}'
        return ''

    def priv_suffix(self):
        if self.is_private:
            return '-priv'
        return ''

    def suffix(self):
        return self.arch_suffix() + self.priv_suffix()

    @property
    def art_imagestream_name(self):
        return f'{self.major_minor}-art-latest' + self.suffix()

    @property
    def art_imagestream_namespace(self):
        return 'ocp' + self.suffix()

    @property
    def nightly_imagestream_name(self):
        return f'{self.art_imagestream_name}-{self.timestamp}'

    @property
    def release_imagestream_name(self):
        return 'release' + self.suffix()


def parse_nightly_components(nightly_name: str) -> typing.Optional[NightlyComponents]:
    nightly_pattern = r'^(?P<major_minor>\d+\.\d+).0-0.nightly-(?P<arch>s390x|arm64|ppc64le)?-?(?P<priv>priv)?-?(?P<timestamp>.*)'
    match = re.match(nightly_pattern, nightly_name)
    if not match:
        return None

    major_minor = match.group('major_minor')  # e.g. "4.16"
    arch = match.group('arch') or 'amd64'
    is_private = match.group('priv') == 'priv'
    timestamp = match.group('timestamp')

    return NightlyComponents(major_minor=major_minor, arch=arch, is_private=is_private, timestamp=timestamp)


def revert(ctx, to_nightly: str, component_name: str, execute):
    nightly_components = parse_nightly_components(to_nightly)
    if not nightly_components:
        logger.error(f'{to_nightly} does not match expected nightly naming convention')
        return

    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.project(nightly_components.art_imagestream_namespace):
                logger.info(f'Finding image in imagestream created for nightly {to_nightly}: {nightly_components.nightly_imagestream_name}')
                nightly_imagestream = oc.selector(f'imagestream/{nightly_components.nightly_imagestream_name}').object(ignore_not_found=False)
                target_component_pullspec = None
                for tag in nightly_imagestream.model.spec.tags:
                    if tag.name == component_name:
                        target_component_pullspec = tag['from'].name
                        break

                if not target_component_pullspec:
                    logger.error(f'Unable to find component image {component_name} in {nightly_imagestream.qname()}')
                    return

                logger.info(f'Reverting tag {nightly_components.major_minor} nightly component {component_name} to reference nightly {to_nightly} image: {target_component_pullspec}')

                art_imagestream = oc.selector(f'imagestream/{nightly_components.art_imagestream_name}').object(ignore_not_found=False)
                cmd_args = ['--import-mode=PreserveOriginal', '--source=docker', target_component_pullspec, f'{art_imagestream.name()}:{component_name}']

                if execute:
                    oc.invoke('tag', cmd_args=cmd_args)
                    logger.info('The tag has been reverted. Note that the next ART update will restore the tag. Use "lock", before reverting, to prevent this.')
                else:
                    logger.info(f'[dry-run] oc tag with {cmd_args}')

        except (ValueError, OpenShiftPythonException, Exception):
            logger.error(f'Unable to process revert of {nightly_components.major_minor} component {component_name}')
            raise


def bypass(ctx, nightly: str, trigger: str, execute):
    nightly_components = parse_nightly_components(nightly_name=nightly)
    if not nightly_components:
        logger.error(f'{nightly} does not match expected nightly naming convention')
        return

    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.project(nightly_components.art_imagestream_namespace):
                nightly_istag = oc.selector(f'imagestreamtag/{nightly_components.release_imagestream_name}:{nightly}').object()
                annotations = {
                    'release.openshift.io/bypass': 'true'
                }

                if execute:
                    nightly_istag.annotate(annotations=annotations, overwrite=True)
                    logger.info(f'Annotated {nightly_istag.qname()} as bypass')
                else:
                    logger.info(f'[dry-run] oc annotate {nightly_istag.qname()} with annotations {annotations}')

                if trigger:
                    art_imagestream = oc.selector(f'imagestream/{nightly_components.art_imagestream_name}').object(ignore_not_found=False)
                    tag_cmd_args = ['--import-mode=PreserveOriginal', '--source=docker', 'registry.access.redhat.com/ubi9', f'{art_imagestream.name()}:trigger-release-controller']
                    untag_cmd_args = ['-d', f'{art_imagestream.name()}:trigger-release-controller']

                    if execute:
                        logger.info('Triggering a release-controller update cycle.')
                        oc.invoke('tag', cmd_args=tag_cmd_args)
                        time.sleep(4)
                        oc.invoke('tag', cmd_args=untag_cmd_args)
                        logger.info('Triggered a release-controller update cycle.')
                    else:
                        logger.info(f'[dry-run] oc tag with {tag_cmd_args} then {untag_cmd_args}')

        except (ValueError, OpenShiftPythonException, Exception):
            logger.error(f'Unable to perform bypass of {nightly_components.major_minor} nightly {nightly}')
            raise


def keep(ctx, namespace, imagestream, release, delete, execute):
    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.project(namespace):
                if release is not None:
                    tag = oc.selector(f'imagestreamtag/{imagestream}:{release}').object(ignore_not_found=True)
                    if not tag:
                        logger.error(f'Unable to locate imagestreamtag: {namespace}/{imagestream}:{release}')
                        return

                    if execute:
                        if delete:
                            logger.info(f'Removing keep annotation from release: {namespace}/{imagestream}:{release}')
                            tag.annotate(annotations={'release.openshift.io/keep': None})
                        else:
                            logger.info(f'Adding keep annotation to release: {namespace}/{imagestream}:{release}')
                            tag.annotate(annotations={'release.openshift.io/keep': True})
                    else:
                        if delete:
                            logger.info(f'[dry-run] Removing keep annotation from release: {namespace}/{imagestream}:{release}')
                        else:
                            logger.info(f'[dry-run] Adding keep annotation to release: {namespace}/{imagestream}:{release}')
                else:
                    tags = oc.selector(f'imagestreamtags').objects(ignore_not_found=True)

                    logger.info(f'ImagestreamTags with the keep annotation:')
                    for tag in tags:
                        if tag.get_annotation('release.openshift.io/keep') is not None:
                            logger.info(f' - {namespace}/{tag.name()}')

        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to process imagestreamtag: "{namespace}/{imagestream}:{release}"')
            raise e


def approval(ctx, namespace, imagestream, release, team, accept, reject, delete, check, execute):
    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.project(namespace):
                payload = oc.selector(f'releasepayload/{release}').object(ignore_not_found=True)
                if not payload:
                    logger.error(f'Unable to locate imagestreamtag: releasepayload/{release}')
                    return
                if execute:
                    if accept:
                        logger.info(f'Marking releasepayload/{release} as Accepted by {team}')
                        payload.label(labels={f'release.openshift.io/{team}_state': 'Accepted'})
                    if reject:
                        logger.info(f'Marking releasepayload/{release} as Rejected by {team}')
                        payload.label(labels={f'release.openshift.io/{team}_state': 'Rejected'})
                    if delete:
                        logger.info(f'[dry-run] Removing approval by {team} from releasepayload/{release}')
                        payload.label(labels={f'release.openshift.io/{team}_state': None})
                else:
                    if accept:
                        logger.info(f'[dry-run] Marking releasepayload/{release} as Accepted by {team}')
                    if reject:
                        logger.info(f'[dry-run] Marking releasepayload/{release} as Rejected by {team}')
                    if delete:
                        logger.info(f'[dry-run] Removing approval by {team} from releasepayload/{release}')
                if check:
                    logger.info(f'Getting {team} label for releasepayload/{release}')
                    state = payload.get_label(f'release.openshift.io/{team}_state')
                    if state is None:
                        logger.info(f'No approval state from {team} found for releasepayload/{release}')
                    else:
                        logger.info(f'releasepayload/{release} marked as {state} by {team}')


        except (ValueError, OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to process imagestreamtag: "{namespace}/{imagestream}:{release}"')
            raise e


def lock(ctx, namespace, imagestream, execute):
    annotations = {
        'release.openshift.io/mode': 'locked',
        'release.openshift.io/messagePrefix': '<span>&#x1F512; Nightly stream has been temporarily locked by TRT -- ART updates will not be recognized.</span><br>'
    }
    annotate_imagestream(ctx, namespace, imagestream, annotations, execute)


def unlock(ctx, namespace, imagestream, execute):
    annotations = {
        'release.openshift.io/mode-': None,
        'release.openshift.io/messagePrefix-': None
    }
    annotate_imagestream(ctx, namespace, imagestream, annotations, execute)


def annotate_imagestream(ctx, namespace, name, annotations, execute):
    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.project(namespace):
                imagestream = oc.selector(f'imagestream/{name}').object()

                if execute:
                    imagestream.annotate(annotations=annotations, overwrite=True)
                    logger.info(f'Annotated {imagestream.qname()} with {annotations}')
                else:
                    logger.info(f'[dry-run] Annotating imagestream {namespace}/{name} with: {annotations}')

        except (ValueError, OpenShiftPythonException, Exception):
            logger.error(f'Unable to annotate imagestream: {namespace}/{name}')
            raise


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Manually accept or reject release payloads')
    parser.add_argument('-m', '--message', help='Specifies a custom message to include with the update', default=None)
    parser.add_argument('-r', '--reason', help='Specifies a custom reason to include with the update', default=None)
    parser.add_argument('-o', '--output', help='The location where backup files will be stored.  If not specified, a temporary location will be used.', default=None)
    parser.add_argument('--execute', help='Specify to persist changes on the cluster', action='store_true')

    config_group = parser.add_argument_group('Configuration Options')
    config_group.add_argument('-v', '--verbose', help='Enable verbose output', action='store_true')

    ocp_group = parser.add_argument_group('Openshift Configuration Options')
    ocp_group.add_argument('-c', '--context', help='The OC context to use (default is "app.ci")', default='app.ci')
    ocp_group.add_argument('-k', '--kubeconfig', help='The kubeconfig to use (default is "~/.kube/config")', default='')
    ocp_group.add_argument('-n', '--name', help='The product prefix to use (default is "ocp")', choices=SUPPORTED_PRODUCTS, default='ocp')
    ocp_group.add_argument('-i', '--imagestream', help='The name of the release imagestream to use (default is "release")', default='release')
    ocp_group.add_argument('-a', '--architecture', help='The architecture of the release to process (default is "amd64")', choices=SUPPORTED_ARCHITECTURES, default='amd64')
    ocp_group.add_argument('-p', '--private', help='Enable updates of "private" releases', action='store_true')

    subparsers = parser.add_subparsers(title='subcommands', description='valid subcommands', help='Supported operations', required=True)
    accept_parser = subparsers.add_parser('accept', help='Accepts the specified release')
    accept_parser.set_defaults(action='accept')
    accept_parser.add_argument('release', help='The name of the release to accept (i.e. 4.10.0-0.ci-2021-12-17-144800)')

    reject_parser = subparsers.add_parser('reject', help='Rejects the specified release')
    reject_parser.set_defaults(action='reject')
    reject_parser.add_argument('release', help='The name of the release to reject (i.e. 4.10.0-0.ci-2021-12-17-144800)')

    prune_parser = subparsers.add_parser('prune', help='Prunes the specified release(s)')
    prune_parser.set_defaults(action='prune')
    prune_parser.add_argument('releases', help='The name of the release(s) to prune (i.e. 4.10.0-0.ci-2021-12-17-144800)', action="extend", nargs="+", type=str)
    prune_parser.add_argument('-y', '--yes', help='Automatically answer yes to confirm deletion(s)', action='store_true')

    archive_parser = subparsers.add_parser('archive', help='Archives tags from the specified imagestream')
    archive_parser.set_defaults(action='archive')
    archive_parser.add_argument('prefixes', help='The prefixes of the tags to archive (i.e. 4.1)', action="extend", nargs="+", type=str)
    archive_parser.add_argument('-y', '--yes', help='Automatically answer yes to confirm deletion(s)', action='store_true')

    revert_parser = subparsers.add_parser('revert', help='Revert a nightly imagestream tag to a previous release')
    revert_parser.set_defaults(action='revert')
    revert_parser.add_argument('--component', help='The release payload component name to revert')
    revert_parser.add_argument('--to-nightly', help='The name of the nightly to revert to (e.g. 4.18.0-0.nightly-2024-09-20-121311)')

    bypass_parser = subparsers.add_parser('bypass', help='Ignore testing in specified nightly and allow a new nightly to be created')
    bypass_parser.set_defaults(action='bypass')
    bypass_parser.add_argument('--nightly', help='The name of the nightly to bypass (e.g. 4.18.0-0.nightly-2024-09-20-121311)')
    bypass_parser.add_argument('--trigger', help='Ask the release-controller to create a new nightly', action='store_true')

    import_parser = subparsers.add_parser('import', help='Manually import all tags defined in the respective imagestream')
    import_parser.set_defaults(action='import')
    import_parser.add_argument('imagestream', help='The name of the imagestream to process (e.g. 4.13-art-latest)')

    keep_parser = subparsers.add_parser('keep',
                                        help='Add/Delete the keep annotation from the respective release or list all imagestreamtags with keep annotation if no options are specified')
    keep_parser.set_defaults(action='keep')
    keep_parser.add_argument('-d', '--delete', help='Remove the keep annotation', action='store_true')
    keep_parser.add_argument('-r', '--release', help='The name of the release (i.e. 4.13.0-0.nightly-2023-10-24-100542)', default=None)

    approval_parser = subparsers.add_parser('approval', help='Mark a release as Accepted or Rejected by a team.')
    approval_parser.set_defaults(action='approval')
    approval_parser.add_argument('release', help='The release to modify team approvals of', nargs="+", type=str)
    approval_parser.add_argument('-a', '--accept_team', help='Add approval from team')
    approval_parser.add_argument('-r', '--reject_team', help='Add rejection from team')
    approval_parser.add_argument('-d', '--delete_team', help='Delete accepted/rejected labels from team')
    approval_parser.add_argument('-c', '--check_team', help='Print team approval for release')

    lock_parser = subparsers.add_parser('lock', help='Lock a nightly release stream.')
    lock_parser.set_defaults(action='lock')
    lock_parser.add_argument('version', help='The version of the nightly release stream to lock (i.e. 4.18)', type=str)

    lock_parser = subparsers.add_parser('unlock', help='Unlock a nightly release stream.')
    lock_parser.set_defaults(action='unlock')
    lock_parser.add_argument('version', help='The version of the nightly release stream to unlock (i.e. 4.18)', type=str)

    args = vars(parser.parse_args())

    if args['verbose']:
        logger.setLevel(logging.DEBUG)

    # Validate the connection to the respective cluster
    context = {"context": args['context']}

    if len(args['kubeconfig']) > 0:
        context['kubeconfig'] = args['kubeconfig']

    validate_server_connection(context)

    # Configure the output location
    if args['output'] is None:
        output_dir = tempfile.mkdtemp(prefix=f'release-tool_{args["action"]}-')
        pass
    else:
        output_dir = args['output']
        if not os.path.isdir(args['output']):
            os.makedirs(output_dir, exist_ok=True)

    logger.info(f'Using output directory: {output_dir}')

    # Get the appropriate release imagestream information
    release_namespace, release_image_stream = generate_resource_values(args['name'], args['imagestream'], args['architecture'], args['private'])

    # Execute action
    if args['action'] in ['accept', 'reject']:
        # TODO: Remove once ReleasePayloads are fully implemented...
        patch_imagestreamtag(context, release_namespace, release_image_stream, args['action'], args['release'], args['message'], args['reason'], args['execute'], output_dir)

        patch_releaespayload(context, release_namespace, args['action'], args['release'], args['reason'], args['execute'], output_dir)
    elif args['action'] == 'prune':
        prune_release_tags(context, release_namespace, release_image_stream, args['releases'], args['execute'], args['yes'], output_dir)
    elif args['action'] == 'archive':
        validate_prefixes(args['prefixes'])
        archive(context, release_namespace, release_image_stream, args['prefixes'], args['execute'], args['yes'], output_dir)
    elif args['action'] == 'import':
        reimport(context, release_namespace, release_image_stream, args['execute'])
    elif args['action'] == 'revert':
        revert(context, args['to_nightly'], args['component'], args['execute'])
    elif args['action'] == 'bypass':
        bypass(context, args['nightly'], args['trigger'], args['execute'])
    elif args['action'] == 'keep':
        keep(context, release_namespace, release_image_stream, args['release'], args['delete'], args['execute'])
    elif args['action'] == 'approval':
        accept = False
        reject = False
        delete = False
        check = False
        team = ''
        if args['accept_team'] is not None:
            team = args['accept_team']
            accept = True
        if args['reject_team'] is not None:
            if team != '':
                logger.error('Only one of `accept-team`, `reject-team`, `delete-team`, or `check-team` may be used')
                exit(1)
            team = args['reject_team']
            reject = True
        if args['delete_team'] is not None:
            if team != '':
                logger.error('Only one of `accept-team`, `reject-team`, `delete-team`, or `check-team` may be used')
                exit(1)
            team = args['delete_team']
            delete = True
        if args['check_team'] is not None:
            if team != '':
                logger.error('Only one of `accept-team`, `reject-team`, `delete-team`, or `check-team` may be used')
                exit(1)
            team = args['check_team']
            check = True
        approval(context, release_namespace, release_image_stream, args['release'][0], team, accept, reject, delete, check, args['execute'])
    elif args['action'] == 'lock':
        # Generate the nightly imagestream information based on version
        nightly_namespace, nightly_imagestream = generate_resource_values(args['name'], f'{args["version"]}-art-latest', args['architecture'], args['private'])
        lock(context, nightly_namespace, nightly_imagestream, args['execute'])
    elif args['action'] == 'unlock':
        # Generate the nightly imagestream information based on version
        nightly_namespace, nightly_imagestream = generate_resource_values(args['name'], f'{args["version"]}-art-latest', args['architecture'], args['private'])
        unlock(context, nightly_namespace, nightly_imagestream, args['execute'])
