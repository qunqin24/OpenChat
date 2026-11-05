---
name: github-work
description: How to work on GitHub with the GitHub plugin - finding and reading repositories, code, issues, pull requests, commits and Actions runs, and writing issues, comments and pull request reviews. Use whenever the user mentions GitHub, a repository (owner/name or a github.com link), an issue or PR number, code review, or their own GitHub activity.
---

# Working on GitHub

The GitHub plugin acts as the user, with the access their own account has. Its tool names change between versions of GitHub's service: when you are not sure which tool does something, look it up with `search("issue comment", { plugin: "github" })` instead of guessing a name.

## Find out who and where first

- "My" repositories, issues or pull requests: get the account with the tool that returns the authenticated user (`get_me`) before searching, and use its login.
- A repository is `owner/name`. Take it from a link the user gave (`github.com/owner/name/...`) rather than guessing; if only a name is given and it is ambiguous, search repositories and say which one you picked.

## Search with GitHub's syntax

Issue, pull request and code searches take GitHub's search qualifiers. Use them instead of fetching long lists and filtering yourself:

- issues and PRs: `repo:owner/name is:issue is:open label:bug`, `is:pr is:merged author:login`, `involves:login`, `updated:>2025-01-01`, `in:title crash`
- code: `repo:owner/name path:src language:go funcName`, `org:name filename:Dockerfile`

Results are paged. Ask for what you need (for example, the most recently updated 20), and say if you looked at only part of a long list.

## Read before you answer

- Read the file (`get_file_contents` or its equivalent) before explaining code; search results show only fragments. For a large repository, start from the README and the directory listing, then go to the files that matter.
- For a pull request, read its description, the changed files and the diff, and its review comments, before summarising or reviewing it. For an issue, read the comments too: the answer is often there.
- For a failing Actions run, find the failed job and read its log around the error rather than the whole log.
- Link what you refer to: `owner/name#123` for issues and PRs, and file links with line numbers where it helps.

## Changing things

Creating or editing issues, comments, labels, branches, files or pull requests, merging, and closing are done as the user and are visible to others. These calls may wait for the user to allow them.

- Only change things the user asked for. Do not, for example, comment on an issue you were asked to read.
- Write the text first (issue body, comment, PR description, review) and show it in your reply, unless the user already gave you the exact words or asked you to go ahead directly.
- Keep what you write in the repository's language and conventions: look at a few existing issues or PRs and any templates or CONTRIBUTING guide.
- If the user refuses an action, do not try another way to do the same thing.

## Writing well

- **Issue:** a title that states the problem; what happened, what was expected, steps to reproduce, environment and versions; logs in code blocks, trimmed to the relevant part.
- **PR description:** what changes and why, how it was tested, and anything a reviewer should look at closely; link the issue it closes (`Closes #123`).
- **Review:** lead with what blocks merging, then suggestions; point at specific lines; distinguish bugs from preferences; be brief and polite.
