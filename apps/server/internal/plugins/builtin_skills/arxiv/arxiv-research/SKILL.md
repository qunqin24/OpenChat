---
name: arxiv-research
description: How to find, read and cite papers with the arXiv plugin. Use when the user asks about research papers, the literature on a topic, the latest work in a field, a specific paper (by title, author or arXiv id), or wants a paper summarised, compared or explained.
---

# Researching papers on arXiv

The arXiv plugin has three tools: `search_papers`, `get_paper` and `read_paper`. Every request to arXiv waits its turn (arXiv allows one every three seconds for the whole server), so each call takes a few seconds: plan a few good searches rather than many loose ones.

## 1. Search well

arXiv's search is plain keyword matching with a simple ranking. It does not understand questions.

- Use a few distinctive words, not a sentence: `speculative decoding draft model`, not `how do people speed up LLM inference`.
- Quote a known title or a fixed term so it matches as written: `"attention is all you need"`, `"retrieval augmented generation"`.
- Use fields when you know them: `ti:` (title), `au:` (author surname), `abs:` (abstract), `cat:` (category), combined with `AND`, `OR`, `ANDNOT`. Example: `au:hinton AND ti:"capsule"`.
- Narrow with `category` (`cs.CL` language, `cs.LG` machine learning, `cs.CV` vision, `cs.AI`, `cs.IR` retrieval, `stat.ML`, `math.*`, `physics.*`, `q-bio.*`, `econ.*`) and with `from` / `to` days (`2024-01-01`).
- `sort: submitted` gives the newest first. Use it for "latest" or "recent" questions; keep the default relevance otherwise.
- If the first search returns nothing useful, change the words or use synonyms (e.g. "LLM" / "large language model"), drop a filter, or search the title with `ti:`. Do not conclude that no paper exists after one search.

When scripting (`plugin_code`), put the searches you already know you need in one script. They still run one after another, but you avoid extra rounds.

## 2. Choose before reading

Search results show the start of each abstract. Pick the papers that actually answer the question from those: their title, abstract, date and categories. Use `get_paper` when you need the whole abstract, every author, the journal reference or the DOI.

Prefer, in order: the paper the user named; the original paper that introduced a method; well-known or later-published versions (a `journal_ref` means it appeared in a journal or conference); recent surveys for a broad question. Mention when you pick one paper among several and why.

## 3. Read the full text only when needed

The abstract is enough to say what a paper is about. Read with `read_paper` when the user wants the method, the results, the numbers, the limitations, or a careful summary.

- Text comes in parts of about 20,000 characters. Continue with the `next_offset` the last part gave until you have what you need; the method and results are usually in the first two or three parts, and the references at the end are rarely worth reading.
- `source` says whether the text came from arXiv's HTML version or from the PDF. PDF text can garble tables and formulas, so state numbers from it with care.
- Never describe sections you did not read as if you had. If you read only part of a paper, say so.

## 4. Report

- Cite every paper with its title, first authors, year and arXiv id as a link: `[2402.01528](https://arxiv.org/abs/2402.01528)`.
- arXiv papers are preprints. Unless `journal_ref` shows it was published, say it may not be peer-reviewed when that matters (claims of state of the art, medical or safety results).
- Give the paper's own numbers with their setting (model, dataset, metric). Do not round them into stronger claims than the paper makes.
- Answer in the user's language, and keep paper titles and technical terms as written.
