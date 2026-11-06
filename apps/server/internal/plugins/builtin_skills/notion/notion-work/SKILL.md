---
name: notion-work
description: How to work in Notion with the Notion plugin - finding and reading pages and databases, querying database entries, writing and editing pages, adding database entries, and reading and writing comments. Use whenever the user mentions Notion, a Notion page or database (by title or a notion.so / notion.site link), their notes, docs, wiki, tasks or trackers kept in Notion, or asks to save something to Notion.
---

# Working in Notion

The Notion plugin acts as the user, in the workspace they connected, with the access their own account has. Its tool names have hyphens (`notion-search`, `notion-fetch`, `notion-create-pages`), so call them with brackets: `tools.notion["notion-search"]({ query: "launch plan" })`. The set of tools changes between versions of Notion's service and with the workspace's plan; when you are not sure which tool does something, look it up with `search("database query", { plugin: "notion" })` instead of guessing a name.

## Find, then read

- Search with `notion-search` using a few distinctive words or the page's title, not a whole question. If nothing useful comes back, try other words or the title alone before concluding a page does not exist.
- Search results are only titles and fragments. Open the pages that matter with `notion-fetch` (by URL or ID) before answering from them, and open several when the answer may be spread over notes from different dates.
- A link the user gave is the page they mean: fetch it directly instead of searching.
- Fetched pages come back as Notion-flavoured Markdown, with the page's `path` and last-edited time. Use the path and date to tell apart pages with similar titles, and say which one you used. A long page can come back `truncated`, with the IDs of the parts left out: fetch those too when the answer may be there.
- "My" pages, or who someone is: `notion-get-self` gives the connected user and workspace, and `notion-search` with `query_type: "user"` finds a teammate by name or email.
- Link every page you refer to with its URL.

## Databases

- A database holds one or more data sources (`collection://...` in a fetched database). Fetch the database first to learn its data sources, their properties (names, types, the options of select and status fields) and its templates.
- To find entries, use `notion-query-data-sources` with filters on those properties rather than searching and filtering yourself; ask for what you need, and say if you looked at only part of a long result.
- An entry is a page: add one with `notion-create-pages` whose parent is the data source, and change one with `notion-update-page`. Use the property names and option values exactly as the schema has them.

## Changing things

Creating, editing, moving and deleting pages, changing databases, and commenting are done as the user and are visible to everyone the page is shared with. These calls may wait for the user to allow them.

- Only change what the user asked for. Do not, for example, tidy up a page you were asked to read.
- Ask where a new page should go if the user did not say and it is not clear from the context; do not drop it in an arbitrary place. Without a destination it becomes a private page of the user's.
- Write the text first and show it in your reply, unless the user already gave you the words or asked you to go ahead directly. Match the existing page's language, headings and structure.
- Edit an existing page with targeted search-and-replace changes, not by rewriting the whole page, so the rest of it (and anything others wrote) stays as it was. Fetch the page just before editing so the text you replace matches. Check the result's `warnings`: a change whose text did not match was skipped, so fix it and try again rather than reporting success.
- Deleting moves pages to Trash and can be undone, but still only delete when the user asked for that page to be deleted, and name what you deleted.
- If the user refuses an action, do not try another way to do the same thing.

## Limits

Notion limits how fast a connection may call it. Prefer a few well-aimed calls to many loose ones, and do not run dozens of fetches at once; if a call reports a rate limit, wait and do fewer at a time. Some search filters need a paid plan: when a result has a `notices` array saying a filter was dropped, the results are broader than asked, so filter them yourself and tell the user.
