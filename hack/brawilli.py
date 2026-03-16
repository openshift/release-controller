#!/usr/bin/env python3

import argparse
import logging

import openshift_client as oc
from openshift_client import OpenShiftPythonException

logging.basicConfig(level=logging.INFO, format='[%(asctime)s] %(message)s')
logger = logging.getLogger('releaseTool')


def validate_server_connection(ctx):
    with oc.options(ctx), oc.tracking(), oc.timeout(60):
        try:
            username = oc.whoami()
            version = oc.get_server_version()
            logger.debug(f'Connected to APIServer running version: {version}, as: {username}')
        except (ValueError, OpenShiftPythonException) as e:
            logger.error(f"Unable to verify cluster connection using context: \"{ctx['context']}\"")
            raise e


def process_imagestream(ctx, namespace, imagestream):
    items = []

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                result = oc.selector(f'imagestream/{imagestream}').object(ignore_not_found=True)

                if result is not None:
                    for tag in result.model.spec.tags:
                        logger.info(f'Processing: "{namespace}/{imagestream}:{tag.name}')

                        found = False
                        for status_tag in result.model.status.tags:
                            if tag.name == status_tag.tag:
                                found = True

                        if not found:
                            items.append(tag.name)
                else:
                    logger.info(f'Imagestream: "{namespace}/{imagestream}" does not exist.')
        except (ValueError, OpenShiftPythonException) as e:
            logger.error(f'Unable to process imagestream: {e}')
            raise e

    return items


def delete_imagestreamtag(ctx, namespace, imagestream, tag):
    imagestreamtag = f'{namespace}/{imagestream}:{tag}'

    with oc.options(ctx), oc.tracking(), oc.timeout(15):
        try:
            with oc.project(namespace):
                logger.info(f'Deleting imagestreamtag: {imagestreamtag}')
                r = oc.invoke('tag', cmd_args=['-d', f'{imagestreamtag}'])
                if r.status() != 0:
                    logger.error(f'Delete returned: {r.out()}')
        except (ValueError, OpenShiftPythonException) as e:
            logger.error(f'Unable to delete imagestreamtag: {e}')
            raise e


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Manually accept or reject release payloads')

    config_group = parser.add_argument_group('Configuration Options')
    config_group.add_argument('-v', '--verbose', help='Enable verbose output', action='store_true')

    ocp_group = parser.add_argument_group('Openshift Configuration Options')
    ocp_group.add_argument('-c', '--context', help='The OC context to use (default is "app.ci")', default='app.ci')
    ocp_group.add_argument('-k', '--kubeconfig', help='The kubeconfig to use (default is "~/.kube/config")', default='')
    ocp_group.add_argument('-i', '--imagestream',
                           help='The name of the release imagestream to use (default is "release")', default='release')
    ocp_group.add_argument('-n', '--namespace',
                           help='The namespace of the release imagestream to use (default is "ocp")', default='ocp')

    args = vars(parser.parse_args())

    if args['verbose']:
        logger.setLevel(logging.DEBUG)

    # Validate the connection to the respective cluster
    context = {"context": args['context']}

    if len(args['kubeconfig']) > 0:
        context['kubeconfig'] = args['kubeconfig']

    validate_server_connection(context)

    for tag in process_imagestream(context, args['namespace'], args['imagestream']):
        delete_imagestreamtag(context, namespace=args['namespace'], imagestream=args['imagestream'], tag=tag)
