import argparse
import requests

BOT_USERNAME = 'openshift-crt-jira-release-controller'
JIRA_URL = 'https://issues.redhat.com/'

def main():
    parser = argparse.ArgumentParser(description="JIRA Issue Comment Management CLI Tool")
    parser.add_argument('--api-token', required=True, help="JIRA API Token")
    parser.add_argument('--jql', required=True, help="JQL Query")
    parser.add_argument('--target-comment', required=True, help="Target Comment")
    parser.add_argument('--sub-target-comment', required=True, help="Sub-Target Comment")
    args = parser.parse_args()

    API_TOKEN = args.api_token
    JQL_QUERY = args.jql
    TARGET_COMMENT = args.target_comment
    SUB_TARGET_COMMENT = args.sub_target_comment


    api_url = f'{JIRA_URL}/rest/api/2/search'

    headers = {
        'Authorization': f'Bearer {API_TOKEN}'
    }

    start_at = 0
    max_results = 1000
    total_results = max_results  # Set an initial value greater than max_results to enter the loop

    while start_at < total_results:
        payload = {
            'jql': JQL_QUERY,
            'fields': 'key,comment',
            'expand': 'comments',
            'startAt': start_at,
            'maxResults': max_results
        }

        response = requests.get(api_url, params=payload, headers=headers)

        if response.status_code == 200:
            data = response.json()
            total_results = data.get('total', 0)
            issues = data.get('issues', [])

            for issue in issues:
                comments = issue.get('fields', {}).get('comment', {}).get('comments', [])
                comment_count = sum(1 for comment in comments if TARGET_COMMENT in comment.get('body', ''))

                if comment_count > 1:
                    issue_comment_counts[issue['key']] = comment_count

            print("Issues with the target comment appearing more than once:")
            for issue_key, comment_count in issue_comment_counts.items():
                print(f"Issue: {issue_key}, Comment Count: {comment_count}")

            for issue_key, comment_count in issue_comment_counts.items():
                if comment_count > 1:
                    delete_comments(issue_key, headers, SUB_TARGET_COMMENT)

            start_at += max_results

        else:
            print(f"Failed to retrieve data. Status code: {response.status_code}")
            break

def delete_comments(issue_key, headers, SUB_TARGET_COMMENT):
    issue_url = f'{JIRA_URL}/rest/api/2/issue/{issue_key}?fields=comment'
    response = requests.get(issue_url, headers=headers)

    if response.status_code == 200:
        issue_data = response.json()
        issue_comments = issue_data.get('fields', {}).get('comment', {}).get('comments', [])
        matching_401_comments = [
            comment for comment in issue_comments
            if SUB_TARGET_COMMENT in comment.get('body', '') and comment.get('author', {}).get('name') == BOT_USERNAME
        ]

        for matching_comments in matching_401_comments:
            comment_id = matching_comments.get('id')
            comment_delete_url = f'{JIRA_URL}/rest/api/2/issue/{issue_key}/comment/{comment_id}'
            response = requests.delete(comment_delete_url, headers=headers)
            if response.status_code == 204:
                print(f"Deleted comment {comment_id} from issue {issue_key}")
            else:
                print(
                    f"Failed to delete comment {comment_id} from issue {issue_key}. Status code: {response.status_code}")
    else:
        print(f"Failed to fetch issue {issue_key} details. Status code: {response.status_code}")

if __name__ == "__main__":
    issue_comment_counts = {}
    main()
