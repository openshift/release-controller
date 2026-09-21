#!/usr/bin/env python3

import argparse
import json
import logging
import os.path
import re
import tempfile
import time
import typing
from collections import Counter

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
        except (OpenShiftPythonException, Exception) as e:
            logger.error(f"Unable to verify cluster connection using context: \"{ctx['context']}\"")
            raise e


def write_backup_file(path, name, release, data):
    ts = int(round(time.time() * 1000))
    backup_filename = f'{path}/{name}_{release}-{ts}.json'

    with open(backup_filename, mode='w+', encoding='utf-8') as backup:
        logger.debug(f'Creating backup file: {backup_filename}')
        backup.write(json.dumps(data, indent=4))

    return backup_filename


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


def resolve_imagestream_from_releasepayload(ctx, namespace, release):
    """Look up the imagestream name from the ReleasePayload's spec.payloadCoordinates.imagestreamName."""
    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                payload = oc.selector(f'releasepayload/{release}').object(ignore_not_found=True)
                if not payload:
                    return None
                return payload.model.spec.payloadCoordinates.imagestreamName
        except (ValueError, OpenShiftPythonException, Exception):
            return None


def patch_releaespayload(ctx, options, namespace, action, release, custom_reason, execute, output_path):
    patch = create_releasepayload_patch(action, custom_reason)
    logger.debug(f'Generated oc patch:\n{json.dumps(patch, indent=4)}')

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace), oc.options(options):
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

        except (OpenShiftPythonException, Exception) as e:
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
        except (OpenShiftPythonException, Exception) as e:
            logger.exception(f'Unable to delete imagestreamtag: {e}')
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
        except (OpenShiftPythonException, Exception) as e:
            logger.exception(f'Unable to process imagestream: {e}')
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
        except (OpenShiftPythonException, Exception) as e:
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
        except (OpenShiftPythonException, Exception) as e:
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


# ----------------------------------------------------------------------------- #
# reset: purge a release's controller state so it can be cleanly re-promoted
# ----------------------------------------------------------------------------- #
#
# When a promoted release fails its creation job or its blocking verification
# jobs, its controller state becomes "burned" and a same-name re-import will not
# re-run cleanly, because:
#   - the ReleasePayload records terminal / retry-exhausted job results (the retry
#     gate then skips relaunching),
#   - the release tag stays adopted (keeps release.openshift.io/{source,phase}) so
#     the controller never re-adopts it and never creates a fresh ReleasePayload,
#   - the verify ProwJobs are named deterministically (<tag>-<verify>[-<retry>]) and
#     are adopted by name on retry; they are NOT GC'd by the release-controller
#     (only by prow's sinker, ~24h), so a re-import re-adopts the old FAILED runs.
#
# This removes all of that (payload imagestream, release tag, ReleasePayload,
# creation job, verify ProwJobs) across the requested arches, after backing each
# up, so a subsequent ART re-import yields a fresh tag -> re-adopt -> fresh
# ReleasePayload -> fresh verification, without bumping the version number. The
# ART assembly/source imagestream (...-art-assembly-...) is never touched.

RESET_VERIFY_LABELS = {'release.openshift.io/verify': 'true'}
RESET_ACTIVE_PROWJOB_STATES = ('triggered', 'pending', 'running')


def _payload_phase(payload_primitive):
    """Replicate the release-controller GetReleasePhase() precedence from a ReleasePayload."""
    created = False
    for condition in (payload_primitive.get('status', {}).get('conditions') or []):
        ctype, cstatus = condition.get('type'), condition.get('status')
        if ctype == 'PayloadAccepted' and cstatus == 'True':
            return 'Accepted'
        if ctype == 'PayloadRejected' and cstatus == 'True':
            return 'Rejected'
        if ctype == 'PayloadFailed' and cstatus == 'True':
            return 'Failed'
        if ctype == 'PayloadCreated' and cstatus == 'True':
            created = True
    return 'Ready' if created else 'Pending'


def discover_reset_target(ctx, options, product, private, release, arch):
    """Gather the per-arch release-controller resources for a single (release, arch).

    Coordinates are read from the live ReleasePayload when present (so naming
    variants like release-5-multi / ci-release-multi resolve correctly), with
    conventional fallbacks derived from generate_resource_values().
    """
    namespace, stream = generate_resource_values(product, 'release', arch, private)
    target = {
        'release': release, 'arch': arch, 'namespace': namespace, 'stream': stream,
        'job_namespace': ('ci-release' if arch == 'amd64' else f'ci-release-{arch}'),
        'job_name': release,
        'payload_is_present': False, 'status_tags': 0, 'import_failures': 0,
        'releasepayload_present': False, 'rp_phase': None,
        'tag_present': False, 'tag_phase': None, 'keep': None,
        'job_present': False, 'job_status': None,
    }

    with oc.options(ctx), oc.tracking(), oc.timeout(60):
        with oc.project(namespace), oc.options(options):
            rp = oc.selector(f'releasepayload/{release}').object(ignore_not_found=True)
            if rp is not None:
                prim = rp.model._primitive()
                target['releasepayload_present'] = True
                target['rp_phase'] = _payload_phase(prim)
                coords = prim.get('spec', {}).get('payloadCoordinates', {}) or {}
                if coords.get('imagestreamName'):
                    target['stream'] = coords['imagestreamName']
                job_coords = (prim.get('status', {}).get('releaseCreationJobResult', {}) or {}).get('coordinates', {}) or {}
                if job_coords.get('namespace'):
                    target['job_namespace'] = job_coords['namespace']
                if job_coords.get('name'):
                    target['job_name'] = job_coords['name']

            payload_is = oc.selector(f'imagestream/{release}').object(ignore_not_found=True)
            if payload_is is not None:
                target['payload_is_present'] = True
                status_tags = payload_is.model._primitive().get('status', {}).get('tags', []) or []
                target['status_tags'] = len(status_tags)
                target['import_failures'] = sum(
                    1 for st in status_tags for c in (st.get('conditions') or [])
                    if c.get('type') == 'ImportSuccess' and c.get('status') == 'False')

            stream_obj = oc.selector(f'imagestream/{target["stream"]}').object(ignore_not_found=True)
            if stream_obj is not None:
                for spec_tag in (stream_obj.model._primitive().get('spec', {}).get('tags', []) or []):
                    if spec_tag.get('name') == release:
                        annotations = spec_tag.get('annotations') or {}
                        target['tag_present'] = True
                        target['tag_phase'] = annotations.get('release.openshift.io/phase')
                        target['keep'] = annotations.get('release.openshift.io/keep')
                        break

        with oc.project(target['job_namespace']), oc.options(options):
            job = oc.selector(f'job/{target["job_name"]}').object(ignore_not_found=True)
            if job is not None:
                target['job_present'] = True
                js = job.model._primitive().get('status', {})
                target['job_status'] = f"succeeded={js.get('succeeded', 0)} active={js.get('active', 0)} failed={js.get('failed', 0)}"

    return target


def discover_verify_prowjobs(ctx, options, prow_namespace, release):
    """List the verify ProwJobs for a release (label-selected; not arch-scoped)."""
    prowjobs = []
    labels = dict(RESET_VERIFY_LABELS, **{'release.openshift.io/payload': release})
    with oc.options(ctx), oc.tracking(), oc.timeout(60):
        with oc.project(prow_namespace), oc.options(options):
            selector = oc.selector('prowjobs', labels=labels)
            for obj in selector.objects(ignore_not_found=True):
                prim = obj.model._primitive()
                prowjobs.append({
                    'name': prim.get('metadata', {}).get('name'),
                    'state': prim.get('status', {}).get('state', 'unknown'),
                })
    return prowjobs


def render_reset_report(release, targets, prowjobs, prow_namespace):
    """Human-readable report of everything a reset would remove for a release."""
    lines = [f'===== reset report: {release} =====']
    for t in targets:
        bits = [f'  [{t["arch"]:<8}] ns={t["namespace"]}']
        if t['payload_is_present']:
            bits.append(f'payloadIS=yes(tags={t["status_tags"]},importFail={t["import_failures"]})')
        else:
            bits.append('payloadIS=absent')
        bits.append(f'releaseTag={t["tag_phase"] if t["tag_present"] else "absent"}')
        bits.append(f'releasePayload={t["rp_phase"] or "absent"}')
        bits.append(f'creationJob={"[" + t["job_status"] + "]" if t["job_present"] else "absent"}')
        if t['keep']:
            bits.append('KEEP-ANNOTATED(will be skipped)')
        lines.append(' '.join(bits))
    states = Counter(p['state'] for p in prowjobs)
    summary = ', '.join(f'{k}={v}' for k, v in sorted(states.items())) if prowjobs else 'none'
    lines.append(f'  verify prowjobs in {prow_namespace} ({len(prowjobs)}): {summary}')
    active = [p for p in prowjobs if p['state'] in RESET_ACTIVE_PROWJOB_STATES]
    if active:
        lines.append(f'  WARNING: {len(active)} verify prowjob(s) still active — deletion will terminate them')
    return '\n'.join(lines)


def backup_reset_target(ctx, options, target, output_dir):
    """Back up every present resource for a (release, arch) to output_dir."""
    ns, release, stream = target['namespace'], target['release'], target['stream']
    with oc.options(ctx), oc.tracking(), oc.timeout(60):
        with oc.project(ns), oc.options(options):
            payload_is = oc.selector(f'imagestream/{release}').object(ignore_not_found=True)
            if payload_is is not None:
                path = write_backup_file(output_dir, f'{ns}_is', release, payload_is.model._primitive())
                logger.info(f'Backup written to: {path}')
            rp = oc.selector(f'releasepayload/{release}').object(ignore_not_found=True)
            if rp is not None:
                path = write_backup_file(output_dir, f'{ns}_releasepayload', release, rp.model._primitive())
                logger.info(f'Backup written to: {path}')
            stream_obj = oc.selector(f'imagestream/{stream}').object(ignore_not_found=True)
            if stream_obj is not None:
                sp = stream_obj.model._primitive()
                entry = {
                    'stream': stream, 'namespace': ns,
                    'spec_tag': next((x for x in (sp.get('spec', {}).get('tags', []) or []) if x.get('name') == release), None),
                    'status_tag': next((x for x in (sp.get('status', {}).get('tags', []) or []) if x.get('tag') == release), None),
                }
                path = write_backup_file(output_dir, f'{ns}_{stream}-tag', release, entry)
                logger.info(f'Backup written to: {path}')
        with oc.project(target['job_namespace']), oc.options(options):
            job = oc.selector(f'job/{target["job_name"]}').object(ignore_not_found=True)
            if job is not None:
                path = write_backup_file(output_dir, f'{target["job_namespace"]}_job', target['job_name'], job.model._primitive())
                logger.info(f'Backup written to: {path}')


def backup_verify_prowjobs(ctx, options, prow_namespace, release, output_dir):
    labels = dict(RESET_VERIFY_LABELS, **{'release.openshift.io/payload': release})
    with oc.options(ctx), oc.tracking(), oc.timeout(60):
        with oc.project(prow_namespace), oc.options(options):
            objs = [o.model._primitive() for o in oc.selector('prowjobs', labels=labels).objects(ignore_not_found=True)]
            if objs:
                path = write_backup_file(output_dir, f'{prow_namespace}_prowjobs', release, objs)
                logger.info(f'Backup written to: {path}')


def delete_reset_target(ctx, options, target):
    """Delete the per-arch resources: creation job, ReleasePayload, release tag, payload IS."""
    ns, release, stream = target['namespace'], target['release'], target['stream']
    if target['keep']:
        logger.warning(f'{ns}/{stream}:{release} is flagged "keep" — skipping this arch.')
        return
    with oc.options(ctx), oc.tracking(), oc.timeout(120):
        with oc.project(target['job_namespace']), oc.options(options):
            job = oc.selector(f'job/{target["job_name"]}').object(ignore_not_found=True)
            if job is not None:
                logger.info(f'Deleting job: {target["job_namespace"]}/{target["job_name"]}')
                job.delete(ignore_not_found=True)
        with oc.project(ns), oc.options(options):
            rp = oc.selector(f'releasepayload/{release}').object(ignore_not_found=True)
            if rp is not None:
                logger.info(f'Deleting releasepayload: {ns}/{release}')
                rp.delete(ignore_not_found=True)
            if target['tag_present']:
                logger.info(f'Deleting release tag: {ns}/{stream}:{release}')
                oc.invoke('tag', cmd_args=['--delete', f'{stream}:{release}'])
            payload_is = oc.selector(f'imagestream/{release}').object(ignore_not_found=True)
            if payload_is is not None:
                logger.info(f'Deleting payload imagestream: {ns}/{release}')
                payload_is.delete(ignore_not_found=True)


def delete_verify_prowjobs(ctx, options, prow_namespace, release):
    labels = dict(RESET_VERIFY_LABELS, **{'release.openshift.io/payload': release})
    with oc.options(ctx), oc.tracking(), oc.timeout(300):
        with oc.project(prow_namespace), oc.options(options):
            selector = oc.selector('prowjobs', labels=labels)
            count = len(selector.objects(ignore_not_found=True))
            if count > 0:
                logger.info(f'Deleting {count} verify prowjob(s) in {prow_namespace} for {release}')
                selector.delete(ignore_not_found=True)


def confirm_reset(release):
    """Per-release gate: [y]es delete / [w]rite backups only / [n]o skip."""
    i = 1
    while i <= 5:
        answer = input(f'Reset {release}?  [y] delete  /  [w] write backups only  /  [n] skip: ').strip().lower()
        if answer in ('y', 'yes'):
            return 'y'
        if answer in ('w', 'write'):
            return 'w'
        if answer in ('n', 'no', ''):
            return 'n'
        print('Please enter y, w, or n')
        i += 1
    return 'n'


def reset_releases(ctx, options, product, private, prow_namespace, arches, releases, execute, assume_yes, keep_prowjobs, output_dir):
    for release in releases:
        targets = [discover_reset_target(ctx, options, product, private, release, arch) for arch in arches]
        prowjobs = discover_verify_prowjobs(ctx, options, prow_namespace, release)

        report = render_reset_report(release, targets, prowjobs, prow_namespace)
        logger.info('\n' + report)

        if not execute:
            logger.warning(f'[dry-run] {release}: no changes made. Specify "--execute" to apply.')
            continue

        choice = 'y' if assume_yes else confirm_reset(release)
        if choice == 'n':
            logger.info(f'{release}: skipped, no changes made.')
            continue

        # Back up everything (for both "write-only" and "delete").
        for target in targets:
            backup_reset_target(ctx, options, target, output_dir)
        backup_verify_prowjobs(ctx, options, prow_namespace, release, output_dir)
        ts = int(round(time.time() * 1000))
        report_file = f'{output_dir}/reset-report_{release}-{ts}.txt'
        with open(report_file, mode='w', encoding='utf-8') as handle:
            handle.write(report + '\n')
        logger.info(f'Report written to: {report_file}')

        if choice == 'w':
            logger.info(f'{release}: report and backups written to {output_dir}; no resources deleted.')
            continue

        if not keep_prowjobs:
            delete_verify_prowjobs(ctx, options, prow_namespace, release)
        for target in targets:
            delete_reset_target(ctx, options, target)
        logger.info(f'{release}: reset complete. Backups in {output_dir}.')


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

                art_imagestream = oc.selector(f'imagestream/{nightly_components.art_imagestream_name}').object(ignore_not_found=False)
                pullspec_to_revert = None
                for tag in art_imagestream.model.spec.tags:
                    if tag.name == component_name:
                        pullspec_to_revert = tag['from'].name
                        break

                if not pullspec_to_revert:
                    logger.error(f'Unable to find component image {component_name} in ART integration stream {art_imagestream.qname()}')
                    return

                logger.info(f'Reverting tag {nightly_components.major_minor} nightly component {component_name} from {pullspec_to_revert} to reference nightly {to_nightly} image: {target_component_pullspec}')

                art_istag_name = f'{art_imagestream.name()}:{component_name}'
                tag_args = ['--import-mode=PreserveOriginal', '--source=docker', target_component_pullspec, art_istag_name]
                annotate_args = ['--overwrite', f'istag/{art_istag_name}', f'reverted-from={pullspec_to_revert}']

                if execute:
                    oc.invoke('tag', cmd_args=tag_args)
                    oc.invoke('annotate', cmd_args=annotate_args)
                    logger.info('The tag has been reverted. Note that the next ART update will restore the tag. Use "lock", before reverting, to prevent this.')
                else:
                    logger.info(f'[dry-run] oc tag with {tag_args}')
                    logger.info(f'[dry-run] oc annotate with {annotate_args}')

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

        except (OpenShiftPythonException, Exception):
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

        except (OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to process imagestreamtag: "{namespace}/{imagestream}:{release}"')
            raise e


def approval(ctx, options, namespace, release, team, accept, reject, delete, check, execute):
    team_label = team.lower()
    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.options(options), oc.project(namespace):
                payload = oc.selector(f'releasepayload/{release}').object(ignore_not_found=True)
                if not payload:
                    logger.error(f'Unable to locate releasepayload/{release}')
                    return
                if execute:
                    if accept:
                        logger.info(f'Marking releasepayload/{release} as Accepted by {team}')
                        payload.label(labels={f'release.openshift.io/{team_label}_state': 'Accepted'})
                    if reject:
                        logger.info(f'Marking releasepayload/{release} as Rejected by {team}')
                        payload.label(labels={f'release.openshift.io/{team_label}_state': 'Rejected'})
                    if delete:
                        logger.info(f'Removing approval by {team} from releasepayload/{release}')
                        payload.label(labels={f'release.openshift.io/{team_label}_state': None})
                else:
                    if accept:
                        logger.info(f'[dry-run] Marking releasepayload/{release} as Accepted by {team}')
                    if reject:
                        logger.info(f'[dry-run] Marking releasepayload/{release} as Rejected by {team}')
                    if delete:
                        logger.info(f'[dry-run] Removing approval by {team} from releasepayload/{release}')
                if check:
                    logger.info(f'Getting {team} label for releasepayload/{release}')
                    state = payload.get_label(f'release.openshift.io/{team_label}_state')
                    if state is None:
                        logger.info(f'No approval state from {team} found for releasepayload/{release}')
                    else:
                        logger.info(f'releasepayload/{release} marked as {state} by {team}')

        except (OpenShiftPythonException, Exception) as e:
            logger.error(f'Unable to process releasepayload: "{namespace}/releasepayload/{release}"')
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


def poke(ctx, namespace, art_imagestream_name, execute):
    with oc.options(ctx), oc.tracking(), oc.timeout(5 * 60):
        try:
            with oc.project(namespace):
                art_imagestream_name = oc.selector(f'imagestream/{art_imagestream_name}').object(ignore_not_found=False)
                tag_cmd_args = ['--import-mode=PreserveOriginal', '--source=docker', 'registry.access.redhat.com/ubi9', f'{art_imagestream_name.name()}:trigger-release-controller']
                untag_cmd_args = ['-d', f'{art_imagestream_name.name()}:trigger-release-controller']

                if execute:
                    logger.info('Triggering a release-controller update cycle.')
                    oc.invoke('tag', cmd_args=tag_cmd_args)
                    time.sleep(4)
                    oc.invoke('tag', cmd_args=untag_cmd_args)
                    logger.info('Triggered a release-controller update cycle.')
                else:
                    logger.info(f'[dry-run] oc tag with {tag_cmd_args} then {untag_cmd_args}')
        except (OpenShiftPythonException, Exception):
            logger.error(f'Unable to poke imagestream: {namespace}/{art_imagestream_name}')
            raise


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

        except (OpenShiftPythonException, Exception):
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
    config_group.add_argument('--admin', help='Run commands as "system:admin"', action='store_true')

    ocp_group = parser.add_argument_group('Openshift Configuration Options')
    ocp_group.add_argument('-c', '--context', help='The OC context to use (default is "app.ci")', default='app.ci')
    ocp_group.add_argument('-k', '--kubeconfig', help='The kubeconfig to use (default is "~/.kube/config")', default='')
    ocp_group.add_argument('-n', '--name', help='The product prefix to use (default is "ocp")', choices=SUPPORTED_PRODUCTS, default='ocp')
    ocp_group.add_argument('-i', '--imagestream', help='The name of the release imagestream to use', default=None)
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

    reset_parser = subparsers.add_parser('reset',
                                         help='Purge a release\'s controller state (payload imagestream, release tag, '
                                              'ReleasePayload, creation job, verify ProwJobs) across arches so it can be '
                                              'cleanly re-promoted without a version bump')
    reset_parser.set_defaults(action='reset')
    reset_parser.add_argument('releases', help='Version(s) to reset (e.g. 4.19.48 5.0.0-rc.3)', nargs='+', type=str)
    reset_parser.add_argument('--arches', help='Arches to process (default: all supported)', nargs='+',
                              choices=SUPPORTED_ARCHITECTURES, default=SUPPORTED_ARCHITECTURES)
    reset_parser.add_argument('--prow-namespace', help='Namespace holding verify ProwJobs (default: "ci")', default='ci')
    reset_parser.add_argument('-y', '--yes', help='Skip the per-release prompt (assume "y"/delete)', action='store_true')
    reset_parser.add_argument('--keep-prowjobs', help='Do not delete verify ProwJobs', action='store_true')

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
    lock_parser.add_argument('version', help='The version of the nightly release stream to lock (e.g. 4.18)', type=str)

    lock_parser = subparsers.add_parser('unlock', help='Unlock a nightly release stream.')
    lock_parser.set_defaults(action='unlock')
    lock_parser.add_argument('version', help='The version of the nightly release stream to unlock (e.g. 4.18)', type=str)

    poke_parser = subparsers.add_parser('poke', help='Poke a nightly imagestream to force a new nightly if none is in progress.')
    poke_parser.set_defaults(action='poke')
    poke_parser.add_argument('version', help='The version of the nightly release to poke (e.g. 4.18)', type=str)

    args = vars(parser.parse_args())

    if args['verbose']:
        logger.setLevel(logging.DEBUG)

    # Validate the connection to the respective cluster
    context = {"context": args['context']}

    if len(args['kubeconfig']) > 0:
        context['kubeconfig'] = args['kubeconfig']

    validate_server_connection(context)

    # Allow for a system:admin override if necessary
    options = {}
    if args['admin']:
        options['as'] = 'system:admin'

    # Configure the output location
    if args['output'] is None:
        output_dir = tempfile.mkdtemp(prefix=f'release-tool_{args["action"]}-')
        pass
    else:
        output_dir = args['output']
        if not os.path.isdir(args['output']):
            os.makedirs(output_dir, exist_ok=True)

    logger.info(f'Using output directory: {output_dir}')

    # Resolve the release namespace and imagestream name.
    # If the user explicitly specified -i, use it exclusively. Otherwise, try
    # to resolve the imagestream name from the ReleasePayload when a release
    # name is available.
    if args['imagestream']:
        release_namespace, release_image_stream = generate_resource_values(args['name'], args['imagestream'], args['architecture'], args['private'])
    else:
        release_namespace, release_image_stream = generate_resource_values(args['name'], 'release', args['architecture'], args['private'])

        # Extract a release name (if available) to resolve the imagestream from
        # the ReleasePayload. Actions that operate on a specific release can
        # look up the authoritative imagestream name from the ReleasePayload's
        # spec.payloadCoordinates. The "archive" action only receives version
        # prefixes (e.g. "5.0"), not a specific release name, so it cannot
        # resolve this way and falls back to "release"; use -i for imagestreams
        # other than "release" (e.g. -i release-5).
        release_name = None
        if args['action'] in ['accept', 'reject', 'keep']:
            release_name = args.get('release')
        elif args['action'] == 'prune':
            releases = args.get('releases', [])
            release_name = releases[0] if releases else None
        elif args['action'] == 'approval':
            releases = args.get('release', [])
            release_name = releases[0] if releases else None

        if release_name:
            resolved = resolve_imagestream_from_releasepayload(context, release_namespace, release_name)
            if resolved:
                release_image_stream = resolved
                logger.info(f'Resolved imagestream from ReleasePayload: {release_image_stream}')
            else:
                logger.warning(f'Unable to resolve imagestream from ReleasePayload, falling back to: {release_image_stream}')

    # Execute action
    if args['action'] in ['accept', 'reject']:
        patch_releaespayload(context, options, release_namespace, args['action'], args['release'], args['reason'], args['execute'], output_dir)
    elif args['action'] == 'prune':
        prune_release_tags(context, release_namespace, release_image_stream, args['releases'], args['execute'], args['yes'], output_dir)
    elif args['action'] == 'archive':
        validate_prefixes(args['prefixes'])
        archive(context, release_namespace, release_image_stream, args['prefixes'], args['execute'], args['yes'], output_dir)
    elif args['action'] == 'import':
        reimport(context, release_namespace, release_image_stream, args['execute'])
    elif args['action'] == 'reset':
        reset_releases(context, options, args['name'], args['private'], args['prow_namespace'], args['arches'],
                       args['releases'], args['execute'], args['yes'], args['keep_prowjobs'], output_dir)
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
        approval(context, options, release_namespace, args['release'][0], team, accept, reject, delete, check, args['execute'])
    elif args['action'] == 'lock':
        # Generate the nightly imagestream information based on version
        nightly_namespace, nightly_imagestream = generate_resource_values(args['name'], f'{args["version"]}-art-latest', args['architecture'], args['private'])
        lock(context, nightly_namespace, nightly_imagestream, args['execute'])
    elif args['action'] == 'unlock':
        # Generate the nightly imagestream information based on version
        nightly_namespace, nightly_imagestream = generate_resource_values(args['name'], f'{args["version"]}-art-latest', args['architecture'], args['private'])
        unlock(context, nightly_namespace, nightly_imagestream, args['execute'])
    elif args['action'] == 'poke':
        # Generate the nightly imagestream information based on version
        nightly_namespace, nightly_imagestream = generate_resource_values(args['name'], f'{args["version"]}-art-latest', args['architecture'], args['private'])
        poke(context, nightly_namespace, nightly_imagestream, args['execute'])
