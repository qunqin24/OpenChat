---
name: scholarly-citations
description: How to use the OpenAlex plugin for research questions that need citations - how influential a paper is, who cites it, what it builds on, the most cited work on a topic, a researcher's record, or papers outside arXiv (journals, conferences, books, any field). Use with the arXiv plugin when both are installed.
---

# Citations and scholarly records with OpenAlex

OpenAlex catalogues over 200 million works from every field and publisher, with their citations, references, authors and institutions. It does not hold full texts: use it to find and judge works, and the arXiv plugin (when installed) to read preprints in full.

## Choosing the tool

| Question | Tool |
|---|---|
| Papers on a topic; the most cited or the newest ones | `search_works` (`sort: cited` or `newest`, `from_year`, `type`) |
| One paper's details, authors, citations per year | `get_work` (an OpenAlex id, a DOI or an arXiv id) |
| Who built on a paper; follow-up work | `citing_works` (`sort: cited` for the important ones, `newest` for the latest) |
| What a paper builds on | `references` |
| A researcher's standing and main work | `search_authors`, then `author_works` |

`get_work`, `citing_works` and `references` all take an arXiv id directly, so a paper found on arXiv can be looked up here without searching again.

## Searching

- `search_works` matches words in titles, abstracts and full texts and ranks by relevance. Use a few precise terms, and the field's own vocabulary.
- For "the classic papers on X", sort by `cited`, and set `to_year` to exclude recent work with few citations yet. For "recent work on X", set `from_year` and sort by `newest` or by relevance.
- Citation counts favour older papers. When comparing papers of different ages, say so, and use `cited_by_years` from `get_work` to show momentum.

## Authors

Several researchers can share a name. Tell them apart by institution and topics, and say which one you mean. The h-index and citation counts are OpenAlex's figures, which can differ from Google Scholar's.

## Reporting

- Give each work's title, first author, year and venue, and link it (the `url`, or its DOI as `https://doi.org/...`).
- State citation counts as "about N citations (OpenAlex)": they change daily and differ between databases.
- An abstract is not the paper. When you have read only the abstract, say so; do not invent results or numbers it does not state.
- If a search returns nothing useful, rephrase or widen the years before saying nothing exists.
