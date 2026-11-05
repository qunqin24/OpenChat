---
name: wikipedia-lookup
description: How to use the Wikipedia plugin - looking up people, places, events, organisations, concepts and definitions, checking facts and dates, and reading an article in another language edition. Use when a question is about settled, encyclopedic knowledge rather than news.
---

# Looking things up on Wikipedia

The Wikipedia plugin has `search` and `read_article`, for any language edition.

## Which edition

- Search in the language the subject is best covered in: Chinese topics in `zh`, Japanese ones in `ja`, most international and technical topics in `en`. When one edition has little, try another.
- `zh` gives simplified characters; use `zh-tw` or `zh-hk` when the user writes traditional Chinese.
- `read_article` lists the article's title in other editions (`other_languages`), so you can switch editions without searching again.

## Searching and reading

1. `search` with the subject's name or a few key words. Pick the right article from the titles and snippets: watch for people or places that share a name, and for disambiguation pages.
2. `read_article` with the exact title from the search. Use `intro` for a definition or a quick fact: the lead section usually has it. Read the whole article (in parts, with `next_offset`) only when the question needs detail from later sections.
3. If the article does not answer the question, say so, and search again or try another edition; do not fill the gap from memory as though it came from Wikipedia.

## Answering

- Answer in the user's language, in your own words. Quote only short passages.
- Link the article you used (its `url`).
- Wikipedia changes and can be wrong. For recent events, current office-holders, prices or statistics, prefer web search, or say how current the article's figure is ("as of 2024, according to Wikipedia").
- Where an article flags a claim as disputed, say so.
