---
name: skill-authoring
description: How to write an OpenChat skill (the Agent Skills format). Use when the user asks to create, improve, or turn a way of working into a skill, or asks what a skill is or how to write one.
---

# Writing a skill

A skill is a short set of instructions you load when a task calls for it. The
user saves it in Settings → Skills (or an administrator in the admin panel);
after that, new conversations list it by name and description, and you open it
with the skill tool when a task matches.

## What to produce

Write the skill as one Markdown file named `SKILL.md`, and give it to the user
in a code block (or in a writing block if they want to edit it with you), so
they can paste it into the editor. Do not claim to have saved it: you cannot.

```markdown
---
name: weekly-report
description: Formats the user's weekly status report. Use when the user asks for a weekly report, a status update or a summary of the week's work.
---

# Weekly report

1. Open with one sentence: the most important result of the week.
2. Then three sections, in this order: Done, Next, Risks.
3. ...
```

## The frontmatter

- `name`: lowercase letters, digits and single hyphens, at most 64 characters,
  such as `weekly-report`. It must be unique among the user's skills.
- `description`: what the skill does **and when to use it**, in one or two
  sentences with the words a request would contain. This is the only part you
  see before opening the skill, so a vague description ("Helps with reports")
  means it is never used. At most 1024 characters.

## The body

- Write it for a capable assistant, not for the user: steps, rules, formats,
  examples, and the mistakes to avoid. Be concrete: "three bullet points, each
  under 15 words" rather than "be concise".
- Put what applies every time in SKILL.md. Long reference material (a style
  guide, a glossary, examples) can go in separate files such as
  `references/glossary.md`, mentioned from SKILL.md by path, so they are read
  only when needed. Tell the user to add them with "Add file" in the editor.
- Scripts can be included as reference, but they cannot be run here yet.
- Keep the whole skill short enough to follow: most good skills are under a
  page.

## Working with the user

Ask what the skill is for, when it should apply, and for an example of good
output if they have one. Then draft the SKILL.md, and offer to adjust it. If
what they describe is a one-off instruction rather than a repeatable way of
working, say so: custom instructions or a plain request may suit it better.
